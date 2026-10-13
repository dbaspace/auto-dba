package store

import (
	"auto-dba/internal/config"
)

func loadTestConfig() (*config.Config, error) {
	return config.Load("../../config/config.yaml")
}
