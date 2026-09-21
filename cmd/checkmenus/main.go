package main

import (
	"fmt"
	"os"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

func main() {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		panic(err)
	}
	db, err := store.Open(cfg.DB.Driver, cfg.DB.DSN)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	list, err := db.ListMenus()
	if err != nil {
		panic(err)
	}
	found := false
	for _, m := range list {
		if m.ID == "m-rbac" || m.ParentID == "m-rbac" {
			fmt.Printf("%s parent=%q title=%s sort=%d\n", m.ID, m.ParentID, m.Title, m.SortOrder)
			if m.ID == "m-rbac" {
				found = true
			}
		}
	}
	if !found {
		fmt.Println("m-rbac MISSING")
		os.Exit(2)
	}
	admin, _ := db.GetRoleByCode("admin")
	if admin == nil {
		fmt.Println("admin role missing")
		os.Exit(3)
	}
	ids, _ := db.ListRoleMenuIDs(admin.ID)
	has := false
	for _, id := range ids {
		if id == "m-rbac" {
			has = true
			break
		}
	}
	fmt.Printf("admin has m-rbac: %v\n", has)
	tree, err := db.MenusForRole(admin.ID, true)
	if err != nil {
		panic(err)
	}
	for _, n := range tree {
		if n.Title == "用户权限" || n.Title == "配置" {
			fmt.Printf("tree top: %s children=%d\n", n.Title, len(n.Children))
			for _, c := range n.Children {
				fmt.Printf("  - %s\n", c.Title)
			}
		}
	}
}
