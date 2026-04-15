package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	CookieName    = "ps_session"
	TokenDuration = 7 * 24 * time.Hour

	bcryptCost = 12
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserExists         = errors.New("user already exists")
	ErrUnauthorized       = errors.New("unauthorized")
)

// User represents a row in the users table. PasswordHash is never populated
// outside the auth package.
type User struct {
	ID          int64
	Email       string
	Role        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastLoginAt *time.Time
}

type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// Auth bundles the DB + JWT secret. Inject via server.Config.
type Auth struct {
	db     *sql.DB
	secret []byte
	secure bool // set Secure flag on cookie; true in prod, false in local dev over HTTP
}

func New(db *sql.DB, secret string, secure bool) (*Auth, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT secret must be at least 32 chars (got %d)", len(secret))
	}
	return &Auth{db: db, secret: []byte(secret), secure: secure}, nil
}

// GenerateSecret returns a hex-encoded 32-byte random secret, useful for
// provisioning scripts.
func GenerateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ---- user repo ----

func (a *Auth) CreateUser(ctx context.Context, email, password, role string) (int64, error) {
	email = normalizeEmail(email)
	if role != RoleAdmin && role != RoleUser {
		return 0, fmt.Errorf("invalid role: %s", role)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return 0, err
	}
	res, err := a.db.ExecContext(ctx,
		`INSERT INTO users(email, password_hash, role) VALUES(?,?,?)`,
		email, string(hash), role,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrUserExists
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (a *Auth) SetPassword(ctx context.Context, userID int64, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		string(hash), userID,
	)
	return err
}

func (a *Auth) GetUserByID(ctx context.Context, id int64) (*User, error) {
	row := a.db.QueryRowContext(ctx,
		`SELECT id, email, role, created_at, updated_at, last_login_at FROM users WHERE id = ?`,
		id,
	)
	return scanUser(row)
}

func (a *Auth) getUserWithHash(ctx context.Context, email string) (*User, string, error) {
	var u User
	var hash string
	var lastLogin sql.NullTime
	err := a.db.QueryRowContext(ctx,
		`SELECT id, email, role, created_at, updated_at, last_login_at, password_hash FROM users WHERE email = ?`,
		normalizeEmail(email),
	).Scan(&u.ID, &u.Email, &u.Role, &u.CreatedAt, &u.UpdatedAt, &lastLogin, &hash)
	if err != nil {
		return nil, "", err
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	return &u, hash, nil
}

// Authenticate checks credentials and returns the user on success. Constant-
// time hash verify + identical error for unknown email vs bad password.
func (a *Auth) Authenticate(ctx context.Context, email, password string) (*User, error) {
	user, hash, err := a.getUserWithHash(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		// Run a dummy bcrypt comparison to keep timing similar.
		bcrypt.CompareHashAndPassword([]byte("$2a$12$C6UzMDM.H6dfI/f/IKxGhuaW1dOlS/jz1VLIH6hS1iZPbQ7bQdC5u"), []byte(password))
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	_, _ = a.db.ExecContext(ctx,
		`UPDATE users SET last_login_at = CURRENT_TIMESTAMP WHERE id = ?`, user.ID,
	)
	return user, nil
}

// ---- JWT ----

func (a *Auth) issueToken(u *User) (string, error) {
	now := time.Now()
	claims := Claims{
		Role: u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", u.ID),
			Issuer:    "pelagicsociety",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(TokenDuration)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(a.secret)
}

func (a *Auth) parseToken(raw string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return a.secret, nil
	}, jwt.WithIssuer("pelagicsociety"), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// ---- cookie / session helpers ----

func (a *Auth) SetSessionCookie(w http.ResponseWriter, u *User) error {
	tok, err := a.issueToken(u)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(TokenDuration),
		MaxAge:   int(TokenDuration.Seconds()),
	})
	return nil
}

func (a *Auth) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// ---- middleware / context ----

type ctxKey int

const userCtxKey ctxKey = 1

// WithOptionalUser attaches the authenticated user to the request context if
// a valid session cookie is present. Always calls through to next.
func (a *Auth) WithOptionalUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := a.UserFromRequest(r)
		if u != nil {
			r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole gates a handler to a given role. Redirects unauthenticated
// users to /login, rejects authenticated users with the wrong role as 403.
func (a *Auth) RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := a.UserFromRequest(r)
			if u == nil {
				http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusSeeOther)
				return
			}
			if u.Role != role {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
			next.ServeHTTP(w, r)
		})
	}
}

// UserFrom returns the user from ctx, or nil.
func UserFrom(ctx context.Context) *User {
	u, _ := ctx.Value(userCtxKey).(*User)
	return u
}

func (a *Auth) UserFromRequest(r *http.Request) *User {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	claims, err := a.parseToken(c.Value)
	if err != nil {
		return nil
	}
	var id int64
	if _, err := fmt.Sscanf(claims.Subject, "%d", &id); err != nil {
		return nil
	}
	u, err := a.GetUserByID(r.Context(), id)
	if err != nil {
		return nil
	}
	// Defense-in-depth: make sure role in DB still matches the token's claim,
	// so a demoted admin can't keep their admin session until token expiry.
	if u.Role != claims.Role {
		return nil
	}
	return u
}

// ---- helpers ----

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

func scanUser(row *sql.Row) (*User, error) {
	var u User
	var lastLogin sql.NullTime
	err := row.Scan(&u.ID, &u.Email, &u.Role, &u.CreatedAt, &u.UpdatedAt, &lastLogin)
	if err != nil {
		return nil, err
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	return &u, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE")
}
