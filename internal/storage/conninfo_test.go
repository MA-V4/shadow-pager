package storage

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// secretPassword is a made up password that must never show up in anything we log or return.
const secretPassword = "s3cr3t-P4ssw0rd-do-not-log"

func TestTargetStripsPasswords(t *testing.T) {
	tests := []struct {
		name         string
		connString   string
		wantHost     string
		wantDatabase string
	}{
		{
			name:         "pooled Neon url",
			connString:   "postgres://app_user:" + secretPassword + "@ep-cool-name-pooler.eu-central-1.aws.neon.tech/shadowpager?sslmode=require",
			wantHost:     "ep-cool-name-pooler.eu-central-1.aws.neon.tech",
			wantDatabase: "shadowpager",
		},
		{
			name:         "Supabase pooler on an unusual port",
			connString:   "postgresql://postgres.ref:" + secretPassword + "@aws-0-eu-west-1.pooler.supabase.com:6543/postgres?sslmode=require",
			wantHost:     "aws-0-eu-west-1.pooler.supabase.com:6543",
			wantDatabase: "postgres",
		},
		{
			name:         "keyword form",
			connString:   "host=db.internal port=5432 user=app password=" + secretPassword + " dbname=shadow sslmode=disable",
			wantHost:     "db.internal",
			wantDatabase: "shadow",
		},
		{
			name:         "password with characters that need escaping",
			connString:   "postgres://app:" + secretPassword + "%40%3A%2F@localhost:5433/app?sslmode=disable",
			wantHost:     "localhost:5433",
			wantDatabase: "app",
		},
		{
			name:         "garbage that still holds the password",
			connString:   "postgres://app:" + secretPassword + "@host:notaport/db",
			wantHost:     unknownTarget,
			wantDatabase: unknownTarget,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, database := Target(tt.connString)
			if host != tt.wantHost || database != tt.wantDatabase {
				t.Errorf("Target = (%q, %q), want (%q, %q)", host, database, tt.wantHost, tt.wantDatabase)
			}
			if strings.Contains(host+database, secretPassword) || strings.Contains(host+database, "app_user") {
				t.Errorf("Target = (%q, %q) leaks the user or the password", host, database)
			}
		})
	}
}

func TestSSLMode(t *testing.T) {
	tests := []struct {
		connString    string
		wantMode      string
		wantEncrypted bool
	}{
		{"postgres://u:p@h/db?sslmode=require", "require", true},
		{"postgres://u:p@h/db?sslmode=verify-ca", "verify-ca", true},
		{"postgres://u:p@h/db?sslmode=verify-full", "verify-full", true},
		{"postgresql://u:p@h/db?application_name=x&sslmode=REQUIRE", "require", true},
		{"postgres://u:p@h/db?sslmode=prefer", "prefer", false},
		{"postgres://u:p@h/db?sslmode=allow", "allow", false},
		{"postgres://u:p@h/db?sslmode=disable", "disable", false},
		{"postgres://u:p@h/db", "", false},
		{"postgres://u:p@h/db?sslmode=", "", false},
		{"host=h user=u password=p dbname=db sslmode=require", "require", true},
		{"host=h user=u password=p dbname=db sslmode='verify-full'", "verify-full", true},
		{"host=h user=u password=p dbname=db", "", false},
		{"host=h sslmode=require sslmode=disable", "disable", false},
		{"", "", false},
		{"postgres://u:p@h:bad/db?sslmode=require", "", false},
	}

	for _, tt := range tests {
		if got := SSLMode(tt.connString); got != tt.wantMode {
			t.Errorf("SSLMode(%q) = %q, want %q", tt.connString, got, tt.wantMode)
		}
		if got := EncryptedTransport(tt.connString); got != tt.wantEncrypted {
			t.Errorf("EncryptedTransport(%q) = %v, want %v", tt.connString, got, tt.wantEncrypted)
		}
	}
}

func TestOpenNeverRepeatsThePasswordOfABrokenURL(t *testing.T) {
	broken := []string{
		"postgres://app:" + secretPassword + "@host:notaport/db",
		"postgres://app:" + secretPassword + "%zz@host/db",
		"postgres://app:" + secretPassword + "@host/db?sslmode=nonsense",
		"host=h password=" + secretPassword + " port=notaport",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, connString := range broken {
		_, err := open(t.Context(), connString, logger, retryPolicy{attempts: 1, baseDelay: time.Millisecond})
		if err == nil {
			t.Fatalf("open with a broken url gave no error")
		}
		if !errors.Is(err, ErrInvalidURL) {
			t.Errorf("error %q does not wrap ErrInvalidURL", err)
		}
		if strings.Contains(err.Error(), secretPassword) {
			t.Errorf("the error repeats the password: %q", err)
		}
	}
}
