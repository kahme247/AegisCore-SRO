package model

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"sync"
)

var ErrChatFilterRepositoryUnavailable = errors.New("chat filter repository is unavailable")

type ChatFilter interface {
	Filter(string) (string, bool)
}

type ChatFilterRepository interface {
	Refresh(context.Context) error
	ChatFilter
}

type chatFilterTerm struct {
	match       string
	replacement string
	pattern     *regexp.Regexp
}

type SQLChatFilterRepository struct {
	shard *sql.DB
	mu    sync.RWMutex
	terms []chatFilterTerm
}

func NewChatFilterRepository(shard *sql.DB) (*SQLChatFilterRepository, error) {
	if shard == nil {
		return nil, ErrChatFilterRepositoryUnavailable
	}
	return &SQLChatFilterRepository{shard: shard}, nil
}

func (r *SQLChatFilterRepository) Refresh(ctx context.Context) error {
	if r == nil || r.shard == nil {
		return ErrChatFilterRepositoryUnavailable
	}
	rows, err := r.shard.QueryContext(ctx, "SELECT TERM, REPLACEMENT FROM CHAT_ABUSE_FILTER WHERE ENABLED = 1 ORDER BY PRIORITY DESC, ID ASC")
	if err != nil {
		return err
	}
	defer rows.Close()
	terms := make([]chatFilterTerm, 0)
	for rows.Next() {
		var term, replacement string
		if err := rows.Scan(&term, &replacement); err != nil {
			return err
		}
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if replacement == "" {
			replacement = strings.Repeat("*", len([]rune(term)))
		}
		if strings.Contains(strings.ToLower(replacement), strings.ToLower(term)) {
			replacement = strings.Repeat("*", len([]rune(term)))
		}
		pattern, compileErr := regexp.Compile("(?i)" + regexp.QuoteMeta(term))
		if compileErr != nil {
			return compileErr
		}
		terms = append(terms, chatFilterTerm{match: strings.ToLower(term), replacement: replacement, pattern: pattern})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	r.terms = terms
	r.mu.Unlock()
	return nil
}

func (r *SQLChatFilterRepository) Filter(message string) (string, bool) {
	if r == nil {
		return message, false
	}
	r.mu.RLock()
	terms := append([]chatFilterTerm(nil), r.terms...)
	r.mu.RUnlock()
	filtered := message
	changed := false
	for _, term := range terms {
		pattern := term.pattern
		if pattern == nil {
			pattern = regexp.MustCompile("(?i)" + regexp.QuoteMeta(term.match))
		}
		updated := pattern.ReplaceAllString(filtered, term.replacement)
		if updated != filtered {
			changed = true
			filtered = updated
		}
	}
	return filtered, changed
}
