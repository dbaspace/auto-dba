package main

import (
	"fmt"
	"os"
	"time"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

func main() {
	cfgPath := "config/config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Println("config:", err)
		os.Exit(1)
	}
	db, err := store.Open(cfg.DB.Driver, cfg.DB.DSN)
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer db.Close()
	u, err := db.GetUserByUsername("admin")
	if err != nil || u == nil {
		fmt.Println("no admin")
		os.Exit(1)
	}
	tenants, _ := db.ListUserTenants(u.ID)
	tid := ""
	if len(tenants) > 0 {
		tid = tenants[0].ID
	}

	// SSO/LDAP config cost (platform login should skip these)
	t0 := time.Now()
	_, _ = db.GetSsoConfig(cfg.App.JWTSecret)
	_, _ = db.GetLdapConfig(cfg.App.JWTSecret)
	cfgMS := time.Since(t0).Milliseconds()

	// cold bootstrap
	t0 = time.Now()
	acc, err := db.BootstrapUserAccessFromUser(u, u.ID, tid, true, false)
	coldMS := time.Since(t0).Milliseconds()
	if err != nil {
		fmt.Println("bootstrap:", err)
		os.Exit(1)
	}

	// warm (menu + arg cache)
	t0 = time.Now()
	_, err = db.BootstrapUserAccessFromUser(u, u.ID, tid, true, false)
	warmMS := time.Since(t0).Milliseconds()
	if err != nil {
		fmt.Println("bootstrap warm:", err)
		os.Exit(1)
	}

	t0 = time.Now()
	_, _ = db.MenusForUserInTenant(u.ID, tid, true)
	_, _ = db.ListUserURNsInTenant(u.ID, tid)
	_, _ = db.ListPlatformURNs(u.ID)
	legacyMS := time.Since(t0).Milliseconds()

	fmt.Printf("role=%s tenant=%s menus=%d perms=%d plat=%d\n", u.Role, tid, len(acc.Menus), len(acc.Permissions), len(acc.PlatformPermissions))
	fmt.Printf("sso+ldap_config_ms=%d bootstrap_cold_ms=%d bootstrap_warm_ms=%d legacy_3call_ms=%d\n",
		cfgMS, coldMS, warmMS, legacyMS)
}
