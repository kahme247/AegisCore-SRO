package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	"github.com/Harvi8/AegisCore/framework/security/hashing"
)

// ErrCredentialRepositoryUnavailable is returned when Gateway login cannot
// reach an injected credential repository. Callers must fail closed rather
// than fall back to opening a database handle from a request path.
var ErrCredentialRepositoryUnavailable = errors.New("credential repository is unavailable")

// CredentialRepositoryError retains an operational cause for errors.Is/As
// while ensuring ordinary error rendering cannot disclose database connection
// details. Gateway login failures are logged on a public-facing path.
type CredentialRepositoryError struct {
	Operation string
	Err       error
}

func (e *CredentialRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Operation == "" {
		return "Gateway credential repository failed"
	}
	return fmt.Sprintf("Gateway credential repository %s failed", e.Operation)
}

func (e *CredentialRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// CredentialRepository is the narrow Gateway login persistence boundary. It
// returns an error for operational failures, and a false valid result for an
// absent account or a password mismatch. Implementations must honor ctx.
type CredentialRepository interface {
	Authenticate(ctx context.Context, username, password string) (accountID uint32, valid bool, err error)
}

// AccountCredentialRepository verifies a Gateway account against an injected
// account database pool or transaction. It does not own or close queryer.
type AccountCredentialRepository struct {
	queryer frameworkdb.RowQueryer
}

// NewAccountCredentialRepository constructs the production implementation
// from a long-lived account pool (or a transaction in tests/units of work).
func NewAccountCredentialRepository(queryer frameworkdb.RowQueryer) (*AccountCredentialRepository, error) {
	if queryer == nil {
		return nil, ErrCredentialRepositoryUnavailable
	}
	return &AccountCredentialRepository{queryer: queryer}, nil
}

// Authenticate fetches only the account ID and stored bcrypt hash needed for
// login. It maps a missing row and malformed/non-positive ID to an ordinary
// invalid-credential result so callers do not reveal account existence.
func (r *AccountCredentialRepository) Authenticate(ctx context.Context, username, password string) (uint32, bool, error) {
	if r == nil || r.queryer == nil {
		return 0, false, ErrCredentialRepositoryUnavailable
	}
	if username == "" || password == "" {
		return 0, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var accountID uint64
	var hashedPassword string
	err := r.queryer.QueryRowContext(ctx, select_password, username).Scan(&accountID, &hashedPassword)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, &CredentialRepositoryError{Operation: "query", Err: err}
	}
	if accountID == 0 || accountID > uint64(^uint32(0)) || hashedPassword == "" {
		return 0, false, nil
	}

	if !hashing.CheckPassword(hashedPassword, password) {
		return 0, false, nil
	}
	return uint32(accountID), true, nil
}

// LegacyCredentialRepository adapts the pre-pool DoLogin helper for callers
// that still use the old constructor. New Gateway startup must inject an
// AccountCredentialRepository backed by framework/db.Pools instead. The
// adapter catches the legacy panic path and turns it into an ordinary error so
// login handling remains fail-closed.
type LegacyCredentialRepository struct{}

// NewLegacyCredentialRepository exposes a compatibility adapter while older
// application wiring is migrated to the injected pool-based constructor.
func NewLegacyCredentialRepository() CredentialRepository {
	return LegacyCredentialRepository{}
}

// Authenticate implements CredentialRepository for the legacy DoLogin
// helper. The helper cannot honor cancellation once it has opened a query, but
// cancellation before that point is respected and panic failures never escape
// into the Gateway request handler.
func (LegacyCredentialRepository) Authenticate(ctx context.Context, username, password string) (accountID uint32, valid bool, err error) {
	if username == "" || password == "" {
		return 0, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			accountID = 0
			valid = false
			// A legacy panic can include a DSN or credential detail. Do not retain
			// it in an error that the login boundary may log.
			err = &CredentialRepositoryError{Operation: "legacy lookup", Err: ErrCredentialRepositoryUnavailable}
		}
	}()

	matched, legacyAccountID := DoLogin(username, password)
	if !matched || legacyAccountID <= 0 || uint64(legacyAccountID) > uint64(^uint32(0)) {
		return 0, false, nil
	}
	return uint32(legacyAccountID), true, nil
}
