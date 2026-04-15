package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fhak/pelagicsociety/internal/auth"
	"github.com/fhak/pelagicsociety/internal/content"
	"github.com/fhak/pelagicsociety/internal/db"
	"github.com/fhak/pelagicsociety/internal/gallery"
	"github.com/fhak/pelagicsociety/internal/mail"
	"github.com/fhak/pelagicsociety/internal/media"
	"github.com/fhak/pelagicsociety/internal/server"
	"github.com/fhak/pelagicsociety/internal/socials"
	"golang.org/x/term"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "create-admin":
			if err := runCreateAdmin(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		case "gen-secret":
			s, err := auth.GenerateSecret()
			if err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			fmt.Println(s)
			return
		case "-h", "--help", "help":
			fmt.Println("usage: pelagicsociety            run the web server")
			fmt.Println("       pelagicsociety create-admin <email>  create or reset admin user")
			fmt.Println("       pelagicsociety gen-secret  print a random JWT secret")
			return
		}
	}

	if err := runServer(); err != nil {
		log.Fatal(err)
	}
}

func runServer() error {
	port := envOr("PORT", "8080")
	dbPath := envOr("DB_PATH", "./data/pelagicsociety.db")
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return errors.New("JWT_SECRET is required")
	}
	cookieSecure := envOr("COOKIE_SECURE", "true") != "false"

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	sqlDB, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer sqlDB.Close()

	authSvc, err := auth.New(sqlDB, jwtSecret, cookieSecure)
	if err != nil {
		return fmt.Errorf("init auth: %w", err)
	}

	mailer := mail.New(
		context.Background(),
		envOr("MAIL_FROM", "Pelagic Society <no-reply@pelagicsociety.com>"),
		envOr("MAIL_REPLY_TO", ""),
	)

	contentSvc := content.New(sqlDB)
	galleryRepo := gallery.NewRepo(sqlDB)
	socialsRepo := socials.NewRepo(sqlDB)
	mediaStore := media.New(context.Background(),
		os.Getenv("MEDIA_BUCKET"),
		os.Getenv("MEDIA_CDN_URL"),
	)

	srv, err := server.New(server.Config{
		DB:            sqlDB,
		Mailer:        mailer,
		Auth:          authSvc,
		Content:       contentSvc,
		Gallery:       galleryRepo,
		Socials:       socialsRepo,
		Media:         mediaStore,
		ContactToAddr: envOr("CONTACT_TO", ""),
	})
	if err != nil {
		return fmt.Errorf("init server: %w", err)
	}

	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("pelagicsociety listening on :%s (db=%s)", port, dbPath)
	return httpSrv.ListenAndServe()
}

func runCreateAdmin(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: pelagicsociety create-admin <email>")
	}
	email := strings.TrimSpace(args[0])
	if email == "" || !strings.Contains(email, "@") {
		return errors.New("valid email required")
	}

	dbPath := envOr("DB_PATH", "./data/pelagicsociety.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	sqlDB, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	// Password prompt, no echo. create-admin doesn't need JWT_SECRET to run.
	authSvc, err := auth.New(sqlDB, strings.Repeat("x", 32), false)
	if err != nil {
		return err
	}

	pw1, err := readPassword(fmt.Sprintf("Password for %s: ", email))
	if err != nil {
		return err
	}
	if len(pw1) < 12 {
		return errors.New("password must be at least 12 characters")
	}
	pw2, err := readPassword("Confirm: ")
	if err != nil {
		return err
	}
	if pw1 != pw2 {
		return errors.New("passwords do not match")
	}

	ctx := context.Background()
	id, err := authSvc.CreateUser(ctx, email, pw1, auth.RoleAdmin)
	if errors.Is(err, auth.ErrUserExists) {
		// Reset password instead.
		var existingID int64
		if err := sqlDB.QueryRowContext(ctx, `SELECT id FROM users WHERE email = ? COLLATE NOCASE`, email).Scan(&existingID); err != nil {
			return err
		}
		if err := authSvc.SetPassword(ctx, existingID, pw1); err != nil {
			return err
		}
		// Ensure role is admin.
		if _, err := sqlDB.ExecContext(ctx, `UPDATE users SET role = 'admin', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, existingID); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "✓ admin password reset for %s (id=%d)\n", email, existingID)
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ admin created: %s (id=%d)\n", email, id)
	return nil
}

// stdinReader is shared across readPassword calls so buffered bytes from
// piped input aren't dropped between prompts.
var stdinReader = bufio.NewReader(os.Stdin)

// readPassword prompts on stderr. If stdin is a TTY, input is read without
// echo; otherwise (piped stdin) it's read as a plain line — handy for scripted
// provisioning. The newline terminator is stripped in both cases.
func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	line, err := stdinReader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
