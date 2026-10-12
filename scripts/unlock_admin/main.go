package main

import (
	"fmt"
	"log"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

func main() {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(cfg.DB.Driver, cfg.DB.DSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	u, err := db.GetUserByUsername("admin")
	if err != nil || u == nil {
		log.Fatalf("admin: %v %#v", err, u)
	}
	if err := db.UnlockUser(u.ID); err != nil {
		log.Fatal(err)
	}
	fmt.Println("unlocked", u.ID, u.Username)
}
