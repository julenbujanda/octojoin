package main

import (
	"testing"
	"time"
)

func TestSortHappyHoursPutsBookedSlotFirstEachDay(t *testing.T) {
	day1 := time.Date(2026, 10, 17, 0, 0, 0, 0, ukLocation)
	day2 := day1.AddDate(0, 0, 1)
	slots := []SavingSession{
		{EventID: 1, StartAt: day1.Add(11 * time.Hour)},
		{EventID: 2, StartAt: day1.Add(12 * time.Hour)},
		{EventID: 3, StartAt: day1.Add(13 * time.Hour), Joined: true},
		{EventID: 4, StartAt: day2.Add(11 * time.Hour)},
		{EventID: 5, StartAt: day2.Add(12 * time.Hour), Joined: true},
	}

	sortHappyHours(slots)

	want := []int{3, 1, 2, 5, 4}
	for i, id := range want {
		if slots[i].EventID != id {
			t.Fatalf("position %d: got event %d, want %d", i, slots[i].EventID, id)
		}
	}
}
