// Copyright (C) 2013 Timo Linna. All Rights Reserved.

package nuodb

import (
	"database/sql"
	"log"
	"math"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

const base_dsn = "nuodb://robinh:crossbow@localhost:48004/tests"
const default_dsn = base_dsn + "?timezone=America/Los_Angeles"

const (
	syntaxError      = -1
	compileError     = -4
	conversionError  = -8
	connectionError  = -10
	noSuchTableError = -25
)

func exec(t *testing.T, db *sql.DB, sql string, args ...interface{}) (li, ra int64) {
	result, err := db.Exec(sql, args...)
	if err != nil {
		_, _, line, _ := runtime.Caller(1)
		t.Fatalf("line:%d sql: %s err: %s", line, sql, err)
	}
	li, _ = result.LastInsertId()
	ra, _ = result.RowsAffected()
	return
}

func query(t *testing.T, db *sql.DB, sql string, args ...interface{}) *sql.Rows {
	rows, err := db.Query(sql, args...)
	if err != nil {
		t.Fatal(sql, "=>", err)
	}
	return rows
}

func testConn(t *testing.T) *sql.DB {
	db, err := sql.Open("nuodb", default_dsn)
	if err != nil {
		t.Fatal("sql.Open:", err)
	}
	exec(t, db, "DROP SCHEMA CASCADE IF EXISTS tests")
	exec(t, db, "CREATE SCHEMA tests")
	exec(t, db, "USE tests")
	return db
}

func expectErrorCode(t *testing.T, err error, code int) {
	if err == nil {
		t.Fatal("Expected error")
	}

	nerr, ok := err.(*Error)
	if !ok {
		t.Fatal("Expected error to be *Error")
	}
	if nerr.Code != ErrorCode(code) {
		t.Fatalf("Expected code %d, got %d", code, nerr.Code)
	}
	if nerr.Message == "" {
		t.Fatal("Empty error message")
	}
}

func TestConnectionError(t *testing.T) {
	// Use an invalid IP address to force a connection error
	db, err := sql.Open("nuodb", "nuodb://robinh:crossbow@0.0.0.1:48004/tests")
	if err != nil {
		t.Fatal("sql.Open:", err)
	}

	err = db.Ping()
	expectErrorCode(t, err, connectionError)
}

func TestExecAndQuery(t *testing.T) {
	db := testConn(t)
	defer db.Close()
	id, ra := exec(t, db, "CREATE TABLE FooBar ("+
		"id BIGINT GENERATED BY DEFAULT AS IDENTITY NOT NULL,"+
		"ir INTEGER,"+
		"big BIGINT,"+
		"num NUMBER,"+
		"dec DECIMAL(6,4),"+
		"flo FLOAT,"+
		"dou DOUBLE,"+
		"cha CHAR,"+
		"blo BLOB,"+
		"str STRING,"+
		"bo1 BOOLEAN,"+
		"bo2 BOOLEAN,"+
		"tim TIME,"+
		"dat DATE,"+
		"ts TIMESTAMP(9))")
	if id|ra != 0 {
		t.Fatal(id, ra)
	}
	insert_stmt := "INSERT INTO FooBar (ir,big,num,dec,flo,dou,cha,blo,str,bo1,bo2,tim,dat,ts) " +
		"VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)"
	id, ra = exec(t, db, insert_stmt, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if id != 1 || ra != 1 {
		t.Fatal(id, ra)
	}
	piNum := "3.1415926535897932384626433832795028841"
	now := time.Now() // Insert with local time zone
	values := []interface{}{-12345, int64(2938746529387465), piNum, math.Pi, float32(math.Pi), float64(math.Pi),
		"X", []byte{10, 20, 30, 40}, "Hello, 世界", true, false, now, now, now}
	id, ra = exec(t, db, insert_stmt, values...)
	if id != 2 || ra != 1 {
		t.Fatal(id, ra)
	}

	rows := query(t, db, "SELECT * FROM FooBar WHERE id = ?", id)
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	expected_columns := []string{"ID", "IR", "BIG", "NUM", "DEC", "FLO", "DOU", "CHA",
		"BLO", "STR", "BO1", "BO2", "TIM", "DAT", "TS"}
	for i, c := range columns {
		e := expected_columns[i]
		if c != e {
			t.Fatalf("Col#%d: expected %v, got %v", i+1, e, c)
		}
	}

	if !rows.Next() {
		t.Fatal("Expected rows")
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	var (
		ir, big            int64
		num, dec, cha, str string
		flo                float32
		dou                float64
		blo                []byte
		bo1, bo2           bool
		tim, dat, ts       time.Time
	)
	vars := []interface{}{&id, &ir, &big, &num, &dec, &flo, &dou, &cha,
		&blo, &str, &bo1, &bo2, &tim, &dat, &ts}
	if err := rows.Scan(vars...); err != nil {
		t.Fatal(err)
	}

	// Fetch with connections' time zone
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now = now.In(loc)
	db_date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	expected_values := []interface{}{int64(2), int64(-12345), int64(2938746529387465), piNum, "3.1416",
		float32(math.Pi), float64(math.Pi), "X", []byte{10, 20, 30, 40}, "Hello, 世界", true, false,
		now, db_date, now}

	for i, v := range vars {
		vi := reflect.ValueOf(v).Elem().Interface()
		ei := reflect.ValueOf(expected_values[i]).Interface()
		if vt, ok := v.(*time.Time); ok {
			if !vt.Equal(expected_values[i].(time.Time)) {
				t.Fatalf("time.Time at Col#%d: expected %v, got %v", i+1, ei, vi)
			}
		} else {
			if !reflect.DeepEqual(vi, ei) {
				t.Fatalf("Col#%d: expected %v, got %v", i+1, ei, vi)
			}
		}
	}

	// Empty column names
	rows = query(t, db, "SELECT 12345, current_user FROM dual")
	columns, err = rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	if len(columns[0]) != 0 {
		t.Fatal(columns[0])
	}
	if len(columns[1]) != 0 {
		t.Fatal(columns[1])
	}
}

func TestExecAndQueryError(t *testing.T) {
	db := testConn(t)
	defer db.Close()

	_, err := db.Exec("CALL NotARealFunction()")
	expectErrorCode(t, err, compileError)

	_, err = db.Query("SELECT * FROM tests.NotARealTable")
	expectErrorCode(t, err, noSuchTableError)
}

func TestCommitAndRollback(t *testing.T) {
	db := testConn(t)
	defer db.Close()
	exec(t, db, "CREATE TABLE tests.FooBarTwo ("+
		"id BIGINT GENERATED BY DEFAULT AS IDENTITY NOT NULL,"+
		"big BIGINT NOT NULL,"+
		"str STRING, dou DOUBLE)")

	// Insert but rollback
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec("INSERT INTO tests.FooBarTwo (big) VALUES (?),(?)", 2345345, 8092333)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	rows := query(t, db, "SELECT big FROM tests.FooBarTwo")
	if rows.Next() {
		log.Fatal("Should not have any rows", rows)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}

	// Insert again and commit
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec("INSERT INTO tests.FooBarTwo (big, str, dou) VALUES (?, ?, NULL),(?, ?, ?)",
		7347388, "Howdy", 2341478, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	rows = query(t, db, "SELECT big, str, dou FROM tests.FooBarTwo")
	if !rows.Next() {
		log.Fatal("Should have had rows", rows)
	}
	values := [2]int64{}
	var str sql.NullString
	var dou sql.NullFloat64
	rows.Scan(&values[0], &str, &dou)
	if !rows.Next() {
		log.Fatal("Should have had rows", rows)
	}

	rows.Scan(&values[1], &str, &dou)
	if values != [2]int64{7347388, 2341478} {
		t.Fatal("Unexpected:", values)
	}
	if str.Valid != false {
		t.Fatal("Expected nil string, got", str)
	}
	if dou.Valid != false {
		t.Fatal("Expected nil float64, got", str)
	}
}

func TestBytes(t *testing.T) {
	db := testConn(t)
	defer db.Close()
	exec(t, db, "CREATE TABLE tests.FooBarThree ("+
		"id BIGINT GENERATED BY DEFAULT AS IDENTITY NOT NULL,"+
		"blob1 BLOB, blob2 BLOB NOT NULL, blob3 BLOB NOT NULL DEFAULT 'x')")

	b := []byte{9, 8, 7, 6, 5}

	exec(t, db, "INSERT INTO tests.FooBarThree (blob1, blob2) VALUES (?,?)",
		nil, b)

	rows := query(t, db, "SELECT blob1, blob2, blob3 FROM tests.FooBarThree")
	if !rows.Next() {
		t.Fatal("Should have had rows", rows)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	var b1, b2, b3 []byte
	if err := rows.Scan(&b1, &b2, &b3); err != nil {
		t.Fatal("Failed to scan:", err)
	}
	if len(b1) != 0 {
		t.Fatalf("%#v", b1)
	}
	if !reflect.DeepEqual(b2, b) {
		t.Fatalf("%#v", b2)
	}
	if b3[0] != 'x' {
		t.Fatalf("%#v", b3)
	}
}

type Item struct{ a, b string }

func appendRows(t *testing.T, items []Item, rows *sql.Rows) []Item {
	for rows.Next() {
		if rows.Err() != nil {
			t.Fatal(rows.Err())
		}
		var a, b sql.NullString
		if err := rows.Scan(&a, &b); err != nil {
			t.Fatal(a, b, err)
		}
		item := Item{a: a.String, b: b.String}
		items = append(items, item)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	return items
}

func TestPrepare(t *testing.T) {
	db := testConn(t)
	defer db.Close()
	exec(t, db, "CREATE TABLE tests.FooBarFour ("+
		"id BIGINT GENERATED BY DEFAULT AS IDENTITY NOT NULL,"+
		"str1 STRING, str2 STRING)")

	li, ra := exec(t, db, "INSERT INTO tests.FooBarFour (str1, str2) VALUES (?,?),(?,?),(?,?),(55.0,12.9)",
		"aa1", "bb1", nil, "bb2", nil, "bb3")
	if li != 4 || ra != 4 {
		t.Fatal(li, ra)
	}
	var items []Item
	stmt, err := db.Prepare("SELECT str1, str2 FROM tests.FooBarFour WHERE str1 = ? OR str2 = ?")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	rows, err := stmt.Query("aa1", "bb3")
	if err != nil {
		t.Fatal(err)
	}
	items = appendRows(t, items, rows)
	rows, err = stmt.Query(nil, "12.9")
	if err != nil {
		t.Fatal(err)
	}
	items = appendRows(t, items, rows)
	stmt2, err := db.Prepare("UPDATE tests.FooBarFour SET str1 = ? WHERE str1 IS NULL")
	defer stmt2.Close()
	result, err := stmt2.Exec("X")
	if err != nil {
		t.Fatal(err)
	}
	rows, err = stmt.Query("X", "bb2")
	if err != nil {
		t.Fatal(err)
	}
	items = appendRows(t, items, rows)
	rowsAffected, err := result.RowsAffected()
	if err != nil || rowsAffected != 2 {
		t.Fatal(err, rowsAffected)
	}
	expected_items := []Item{{"aa1", "bb1"}, {"", "bb3"}, {"55.0", "12.9"}, {"X", "bb2"}, {"X", "bb3"}}
	for i, v := range items {
		vi := reflect.ValueOf(v).Interface()
		ei := reflect.ValueOf(expected_items[i]).Interface()
		if !reflect.DeepEqual(vi, ei) {
			t.Fatalf("%d: expected %v, got %v", i, ei, vi)
		}
	}
	stmt3, err := db.Prepare("DELETE FROM tests.FooBarFour WHERE ID < ?")
	if err != nil {
		t.Fatal(err)
	}
	rows, err = stmt3.Query(3) // Delete items 1 and 2 with a Query
	if err != nil {
		t.Fatal(err)
	}
	columns, err := rows.Columns()
	if len(columns) != 0 {
		t.Fatal(columns)
	}
	if rows.Next() {
		t.Fatal("Unexpected values")
	}

	stmt.Query("aa1", "bb1")
	for rows.Next() {
		t.Fatal("Unexpected values")
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	var id int64
	if err = rows.Scan(&id); err == nil {
		t.Fatal(err)
	}
}

func TestPrepareError(t *testing.T) {
	db := testConn(t)
	defer db.Close()
	exec(t, db, "CREATE TABLE tests.FooBarFour ("+
		"id BIGINT GENERATED BY DEFAULT AS IDENTITY NOT NULL,"+
		"str1 STRING, str2 STRING)")

	_, err := db.Prepare("INSERT INTO tests.FooBarFour (ID) VALUES (?, ?)") // Too many placeholders
	expectErrorCode(t, err, syntaxError)

	stmt, err := db.Prepare("INSERT INTO tests.FooBarFour (ID) VALUES (?)")
	_, err = stmt.Exec("NotAnInt")
	expectErrorCode(t, err, conversionError)
}

func TestDDL(t *testing.T) {
	db := testConn(t)
	defer db.Close()
	result, err := db.Exec("  \t  \nCREAte\t  \nTABLE FooBar (id integer)")
	if err != nil {
		t.Fatal(result, err)
	}
	if id, err := result.LastInsertId(); err == nil {
		t.Fatal("DDL statement", id, err)
	}
	if nrows, err := result.RowsAffected(); err == nil {
		t.Fatal("DDL statement", nrows, err)
	}
}

// TestStringSequence is a regression test to ensure there is no failure when inserting into a
// table that defines a column like 'col_name STRING GENERATED BY DEFAULT AS IDENTITY'.
// The code used to assume that all generated keys could be cast to a long, which failed in the
// string case with an error 'unable to convert string "test" into type "bigint"'
func TestStringSequence(t *testing.T) {
	db := testConn(t)
	defer db.Close()

	exec(t, db, "CREATE TABLE tests.FooBarFive ("+
		"id STRING GENERATED BY DEFAULT AS IDENTITY NOT NULL)")

	result, err := db.Exec("INSERT INTO tests.FooBarFive VALUES ('test')")
	if err != nil {
		t.Fatal(result, err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		t.Fatalf("Expected last insert id to be 0, was %d", id)
	}
}

func TestNumericSequence(t *testing.T) {
	db := testConn(t)
	defer db.Close()

	exec(t, db, "CREATE TABLE tests.FooBarSix ("+
		"id INT GENERATED BY DEFAULT AS IDENTITY NOT NULL)")

	result, err := db.Exec("INSERT INTO tests.FooBarSix DEFAULT VALUES")
	if err != nil {
		t.Fatal(result, err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Fatalf("Expected last insert id to be 1, was %d", id)
	}
}

func TestConnectionPropsSchema(t *testing.T) {
	expectedSchema := "tests"
	dsn := default_dsn + "&schema=" + expectedSchema

	db, err := sql.Open("nuodb", dsn)
	if err != nil {
		t.Fatal("sql.Open:", err)
	}

	rows := query(t, db, "SELECT current_schema() FROM DUAL")
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("Expected rows")
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}

	var schema string
	rows.Scan(&schema)
	schema = strings.ToLower(schema)

	if rows.Next() {
		t.Fatal("Unexpected rows")
	}

	if schema != expectedSchema {
		t.Fatalf("Expected schema '%s', was '%s'", expectedSchema, schema)
	}
}

func TestConnectionPropsTimezone(t *testing.T) {
	localZone, _ := time.Now().Zone()
	tests := []struct{ tz, expected string }{
		{"Asia/Tokyo", "JST"},
		{"", localZone},
	}

	for _, test := range tests {
		t.Run(test.expected, func(t *testing.T) {
			dsn := base_dsn + "?timezone=" + test.tz

			db, err := sql.Open("nuodb", dsn)
			if err != nil {
				t.Fatal("sql.Open:", err)
			}

			rows := query(t, db, "SELECT NOW() FROM DUAL")
			defer rows.Close()

			if !rows.Next() {
				t.Fatal("Expected rows")
			}
			if rows.Err() != nil {
				t.Fatal(rows.Err())
			}

			var now time.Time
			rows.Scan(&now)

			if rows.Next() {
				t.Fatal("Unexpected rows")
			}

			zone, _ := now.Zone()
			if zone != test.expected {
				t.Fatalf("Expected TZ '%s', was '%s'", test.expected, zone)
			}
		})
	}
}

func TestConnectionPropsClientInfo(t *testing.T) {
	expectedInfo := "arbitraryinfo"
	dsn := default_dsn + "&clientInfo=" + expectedInfo

	db, err := sql.Open("nuodb", dsn)
	if err != nil {
		t.Fatal("sql.Open:", err)
	}

	rows := query(t, db, "SELECT clientinfo FROM system.localconnections WHERE connid = GETCONNECTIONID()")
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("Expected rows")
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}

	var clientInfo string
	rows.Scan(&clientInfo)

	if rows.Next() {
		t.Fatal("Unexpected rows")
	}

	if clientInfo != expectedInfo {
		t.Fatalf("Expected clientInfo '%s', was '%s'", expectedInfo, clientInfo)
	}
}

func TestConnectionPropsEmpty(t *testing.T) {
	dsn := default_dsn + "&="

	db, err := sql.Open("nuodb", dsn)
	if err != nil {
		t.Fatal("sql.Open:", err)
	}

	rows := query(t, db, "SELECT 1 FROM DUAL")
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("Expected rows")
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	if rows.Next() {
		t.Fatal("Unexpected rows")
	}
}
