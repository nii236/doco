package doco

import (
	"doco/bindata"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	migrate_bindata "github.com/golang-migrate/migrate/v4/source/go_bindata"
	"github.com/jmoiron/sqlx"
)

func randomAvatar() ([]byte, error) {
	resp, err := http.Get("https://i.pravatar.cc/300")
	if err != nil {
		return nil, err
	}
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return b, err
}

func newMigrateInstance(conn *sqlx.DB) (*migrate.Migrate, error) {
	s := migrate_bindata.Resource(bindata.AssetNames(),
		func(name string) ([]byte, error) {
			return bindata.Asset(name)
		})
	d, err := migrate_bindata.WithInstance(s)
	if err != nil {
		return nil, fmt.Errorf("bindata instance: %w", err)
	}
	dbDriver, err := sqlite3.WithInstance(conn.DB, &sqlite3.Config{})
	if err != nil {
		return nil, fmt.Errorf("db instance: %w", err)
	}
	m, err := migrate.NewWithInstance("go-bindata", d, "sqlite", dbDriver)
	if err != nil {
		return nil, fmt.Errorf("migrate instance: %w", err)
	}
	return m, nil
}
func Migrate(conn *sqlx.DB) error {
	m, err := newMigrateInstance(conn)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	err = m.Up()
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
func Drop(conn *sqlx.DB) error {
	m, err := newMigrateInstance(conn)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	err = m.Drop()
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
func Version(conn *sqlx.DB) (uint, bool, error) {
	m, err := newMigrateInstance(conn)
	if err != nil {
		return 0, false, fmt.Errorf("migrate: %w", err)
	}
	v, d, err := m.Version()
	if err != nil {
		return 0, false, fmt.Errorf("migrate: %w", err)
	}
	return v, d, nil
}
func Seed(masterKeyHex string) error {

	return nil
}
