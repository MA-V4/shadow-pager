package storage

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// CONNECTION STRINGS

// ErrInvalidURL means the database address could not be read, and it says no more because the address holds a password.
var ErrInvalidURL = errors.New("the database url is not a valid Postgres connection string (details are withheld because it holds a password)")

// unknownTarget is what we log when the address cannot be read at all.
const unknownTarget = "unknown database"

// Target gives back only the host and the database name of a connection string, which is all that may be logged.
func Target(connString string) (host, database string) {
	cfg, err := pgconn.ParseConfig(connString)
	if err != nil {
		return unknownTarget, unknownTarget
	}
	host = cfg.Host
	// The usual port says nothing new, so only an unusual one is shown.
	if cfg.Port != 0 && cfg.Port != 5432 {
		host = net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port)))
	}
	return host, cfg.Database
}

// SSLMode gives back the sslmode a connection string asks for, or nothing when it does not say.
func SSLMode(connString string) string {
	connString = strings.TrimSpace(connString)
	if strings.HasPrefix(connString, "postgres://") || strings.HasPrefix(connString, "postgresql://") {
		u, err := url.Parse(connString)
		if err != nil {
			return ""
		}
		return strings.ToLower(u.Query().Get("sslmode"))
	}
	// The other form is a list of words like "host=db sslmode=require", where the last sslmode wins.
	mode := ""
	for _, field := range strings.Fields(connString) {
		if key, value, ok := strings.Cut(field, "="); ok && strings.EqualFold(key, "sslmode") {
			mode = strings.ToLower(strings.Trim(value, `'"`))
		}
	}
	return mode
}

// EncryptedTransport tells us if a connection string insists on TLS, which only require and the stricter modes do.
func EncryptedTransport(connString string) bool {
	switch SSLMode(connString) {
	case "require", "verify-ca", "verify-full":
		return true
	}
	return false
}
