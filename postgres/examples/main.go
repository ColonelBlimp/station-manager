package main

import (
	"context"
	"fmt"
	"github.com/Station-Manager/config"
	"github.com/Station-Manager/database"
	"github.com/Station-Manager/logging"
	"os"
)

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return wd
}

func main() {
	_ = os.Setenv("SM_DEFAULT_DB", "pg")

	cfgService := &config.Service{WorkingDir: mustGetwd()}
	if err := cfgService.Initialize(); err != nil {
		panic(err)
	}

	loggingService := &logging.Service{ConfigService: cfgService}
	if err := loggingService.Initialize(); err != nil {
		panic(err)
	}
	defer func() { _ = loggingService.Close() }()

	dbService := database.Service{ConfigService: cfgService, Logger: loggingService}
	if err := dbService.Initialize(); err != nil {
		panic(err)
	}

	fmt.Println("Using DB driver:", cfgService.AppConfig.DatastoreConfig.Driver)
	fmt.Println("Opening DB...")
	if err := dbService.Open(); err != nil {
		panic(err)
	}
	fmt.Println("DB open OK.")

	fmt.Println("Running migrations...")
	if err := dbService.Migrate(); err != nil {
		fmt.Println("migrations failed:", err)
		panic(err)
	}
	fmt.Println("Migrations completed.")

	fmt.Println("Listing tables...")
	ctx := context.Background()
	rows, err := dbService.QueryContext(ctx, `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = current_schema()
		ORDER BY table_name`)
	if err != nil {
		fmt.Println("failed to list tables:", err)
	} else {
		defer func() { _ = rows.Close() }()
		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				fmt.Println("scan tables failed:", err)
				break
			}
			names = append(names, name)
		}
		if err := rows.Err(); err != nil {
			fmt.Println("rows error:", err)
		}
		fmt.Println("current schema tables:", names)
	}

	fmt.Println("Closing DB...")
	if err := dbService.Close(); err != nil {
		panic(err)
	}
	fmt.Println("Done.")
}
