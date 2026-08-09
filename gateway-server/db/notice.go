package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	log "github.com/sirupsen/logrus"
)

type Notice struct {
	Id      int
	Subject string
	Article string
	Ctime   time.Time
}

const selectNotice = "SELECT id, SUBJECT, ARTICLE, CTIME FROM NOTICE ORDER BY CTIME DESC"

var ErrNoticeRepositoryUnavailable = errors.New("notice repository is unavailable")

// NoticeRepositoryError identifies the failed catalog read operation without
// requiring callers to parse a database-driver error string.
type NoticeRepositoryError struct {
	Operation string
	Err       error
}

func (e *NoticeRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("notice repository %s: %v", e.Operation, e.Err)
}

func (e *NoticeRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NoticeCatalog is the Gateway-facing read contract. It is intentionally
// small so handlers can use a long-lived pool in production or a safe fake in
// tests without opening a database connection themselves.
type NoticeCatalog interface {
	List(ctx context.Context) ([]Notice, error)
}

// NoticeCatalogFunc adapts a context-aware function to NoticeCatalog.
type NoticeCatalogFunc func(context.Context) ([]Notice, error)

func (f NoticeCatalogFunc) List(ctx context.Context) ([]Notice, error) {
	return f(ctx)
}

// NoticeRepository receives a long-lived account pool or transaction from its
// caller. It never opens or closes a database connection itself.
type NoticeRepository struct {
	queryer frameworkdb.Queryer
}

func NewNoticeRepository(queryer frameworkdb.Queryer) (*NoticeRepository, error) {
	if queryer == nil {
		return nil, &NoticeRepositoryError{
			Operation: "configure",
			Err:       ErrNoticeRepositoryUnavailable,
		}
	}
	return &NoticeRepository{queryer: queryer}, nil
}

// List reads the current notice catalog and reports query, scan, and row
// iteration failures to the caller instead of turning a bad database response
// into a process-wide panic.
func (r *NoticeRepository) List(ctx context.Context) ([]Notice, error) {
	if r == nil || r.queryer == nil {
		return nil, &NoticeRepositoryError{
			Operation: "query",
			Err:       ErrNoticeRepositoryUnavailable,
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.queryer.QueryContext(ctx, selectNotice)
	if err != nil {
		return nil, &NoticeRepositoryError{Operation: "query", Err: err}
	}
	defer rows.Close()

	notices := make([]Notice, 0)
	for rows.Next() {
		notice := Notice{}
		if err := rows.Scan(&notice.Id, &notice.Subject, &notice.Article, &notice.Ctime); err != nil {
			return nil, &NoticeRepositoryError{Operation: "scan", Err: err}
		}
		notices = append(notices, notice)
	}
	if err := rows.Err(); err != nil {
		return nil, &NoticeRepositoryError{Operation: "iterate", Err: err}
	}
	return notices, nil
}

// LegacyNoticeCatalog keeps older startup paths working until they inject a
// long-lived framework/db pool. New handlers should receive NoticeRepository
// (or another NoticeCatalog) directly; this adapter is deliberately the only
// catalog read path that opens a short-lived connection per request.
func LegacyNoticeCatalog() NoticeCatalog {
	return NoticeCatalogFunc(func(ctx context.Context) ([]Notice, error) {
		conn := frameworkdb.OpenConnAccount()
		defer conn.Close()

		repository, err := NewNoticeRepository(conn)
		if err != nil {
			return nil, err
		}
		return repository.List(ctx)
	})
}

// GetNotices is retained for callers that have not moved to an injected
// NoticeCatalog. The handler path uses List and returns errors; this legacy
// helper logs and returns an empty list to preserve its historical []Notice
// shape without relying on framework/db.CheckError.
func GetNotices() []Notice {
	notices, err := LegacyNoticeCatalog().List(context.Background())
	if err != nil {
		log.WithError(err).Error("load Gateway notices")
		return []Notice{}
	}
	return notices
}
