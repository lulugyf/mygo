package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	_ "gopkg.in/rana/ora.v4"
)

func updateTest(db *sql.DB) {
	stmt, err := db.Prepare("INSERT INTO test_tab(f1, f2) VALUES(:v1, :v2)")
	if err != nil {
		log.Fatal(err)
	}
	res, err := stmt.Exec(-2, "Dolly")
	if err != nil {
		log.Fatal(err)
	}
	lastId, err := res.LastInsertId()
	if err != nil {
		log.Fatal(err)
	}
	rowCnt, err := res.RowsAffected()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("ID = %d, affected = %d\n", lastId, rowCnt)
}

func selTest(db *sql.DB, ctx context.Context) {
	var (
		f1 int
		f2 string
	)
	rows, err := db.QueryContext(ctx, "select f1, f2 from test_tab where f1 > :v1", 50)
	if err != nil {
		log.Fatalf("failed1 %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&f1, &f2)
		if err != nil {
			log.Fatal(err)
		}
		log.Println(f1, f2)
	}
}

func main() {

	db, err := sql.Open("ora", "crm/crm@127.0.0.1:1521/xe")
	if err != nil {
		log.Fatalf("connect db failed: %v", err)
	}
	defer db.Close()

	// Set timeout (Go 1.8)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Set prefetch count (Go 1.8)
	//	ctx = ora.WithStmtCfg(ctx, ora.Cfg().StmtCfg.SetPrefetchCount(50000))

	updateTest(db)

	selTest(db, ctx)

	log.Printf("done!")
}
