package tui

import (
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestRenderListScrollsToCursor(t *testing.T) {
	ny := testutil.NY(t)
	from := time.Date(2026, 11, 9, 0, 0, 0, 0, ny)
	var events []model.Event
	for d := 9; d < 16; d++ {
		events = append(events, testutil.Timed("e", "Day "+time.Date(2026, 11, d, 0, 0, 0, 0, ny).Format("02"),
			time.Date(2026, 11, d, 9, 0, 0, 0, ny), time.Hour))
	}

	got := renderList(listView{days: model.GroupByDay(events, from, 7), cursor: 6, width: 40, height: 5, loc: ny})

	testutil.Golden(t, "list_scrolled", got)
}
