package store

import (
	"cmp"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/jabiOP/clindar/internal/model"
)

// notFound converts sql.ErrNoRows into our ErrNotFound so callers never import database/sql.
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func sortCalendars(cals []model.Calendar) {
	slices.SortFunc(cals, func(a, b model.Calendar) int {
		if a.Primary != b.Primary {
			if a.Primary {
				return -1
			}
			return 1
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
}
