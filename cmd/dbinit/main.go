// Command dbinit creates the configured PostgreSQL database when it is absent.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"

	"github.com/jackc/pgx/v5"
)

func main() {
	dsn := flag.String("dsn", "", "PostgreSQL DSN for the target database")
	flag.Parse()
	parsed, err := url.Parse(*dsn)
	if err != nil || parsed.Path == "" {
		log.Fatal("无效 DSN")
	}
	database := parsed.Path[1:]
	parsed.Path = "/postgres"
	conn, err := pgx.Connect(context.Background(), parsed.String())
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close(context.Background())
	var exists bool
	err = conn.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", database).Scan(&exists)
	if err != nil {
		log.Fatal(err)
	}
	if !exists {
		_, err = conn.Exec(context.Background(), "CREATE DATABASE "+pgx.Identifier{database}.Sanitize())
		if err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("database %s ready\n", database)
}
