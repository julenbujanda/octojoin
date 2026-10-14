// Copyright 2025 Matthew Gall <me@matthewgall.dev>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNtfyNotifierSend(t *testing.T) {
	var gotBody, gotTitle, gotTags, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotTitle = r.Header.Get("Title")
		gotTags = r.Header.Get("Tags")
		gotAuth = r.Header.Get("Authorization")
	}))
	defer server.Close()

	n := NewNtfyNotifier(server.URL+"/topic", "tk_secret")
	err := n.Send(NtfyMessage{Title: "Hello", Message: "World", Tags: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if gotBody != "World" || gotTitle != "Hello" || gotTags != "a,b" || gotAuth != "Bearer tk_secret" {
		t.Errorf("unexpected request: body=%q title=%q tags=%q auth=%q", gotBody, gotTitle, gotTags, gotAuth)
	}
}

func TestNtfyNotifierSendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	if err := NewNtfyNotifier(server.URL, "").Send(NtfyMessage{Message: "x"}); err == nil {
		t.Error("expected error for non-2xx response")
	}
}

func newTestMonitor(notifier *NtfyNotifier) *SavingSessionMonitor {
	return &SavingSessionMonitor{
		state:            &AppState{KnownSessions: make(map[int]bool)},
		logger:           NewLogger(false),
		daemonMode:       true,
		notifier:         notifier,
		happyHoursPerDay: 1,
		bookHappyHour:    func(string) error { return nil },
	}
}

// upcomingDay is far enough ahead for its slots to be bookable
func upcomingDay() time.Time {
	return time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, 3)
}

func happyHourSlots(day time.Time, firstID int, bookedIndex int) []SavingSession {
	var slots []SavingSession
	for i := 0; i < 4; i++ {
		start := day.Add(time.Duration(10+i) * time.Hour)
		slots = append(slots, SavingSession{
			EventID:   firstID + i,
			Code:      fmt.Sprintf("EVENT_%d", firstID+i),
			StartAt:   start,
			EndAt:     start.Add(time.Hour),
			EventType: SavingSessionTypeWeekendHappyHour,
			Joined:    i == bookedIndex,
		})
	}
	return slots
}

func TestCheckWeekendHappyHoursNotifiesOncePerDay(t *testing.T) {
	var messages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		messages = append(messages, r.Header.Get("Title")+"|"+string(body))
	}))
	defer server.Close()

	m := newTestMonitor(NewNtfyNotifier(server.URL, ""))
	saturday := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	sunday := saturday.AddDate(0, 0, 1)
	slots := append(happyHourSlots(saturday, 100, 3), happyHourSlots(sunday, 200, -1)...)

	if !m.checkWeekendHappyHours(slots, 0) {
		t.Error("expected new happy hours to be reported")
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 notifications (one per day), got %d: %v", len(messages), messages)
	}
	if !strings.Contains(messages[0], "Saturday, Oct 10") || !strings.Contains(messages[0], "14:00-15:00 (booked)") {
		t.Errorf("unexpected Saturday notification: %q", messages[0])
	}
	if !strings.Contains(messages[1], "Book your slot") {
		t.Errorf("expected unbooked Sunday notification to prompt booking: %q", messages[1])
	}

	// Seen slots should not be notified again
	if m.checkWeekendHappyHours(slots, 0) {
		t.Error("expected no new happy hours on second check")
	}
	if len(messages) != 2 {
		t.Errorf("expected no further notifications, got %d", len(messages))
	}
}

func TestCheckWeekendHappyHoursRetriesFailedNotification(t *testing.T) {
	fail := true
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	m := newTestMonitor(NewNtfyNotifier(server.URL, ""))
	slots := happyHourSlots(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), 100, -1)

	m.checkWeekendHappyHours(slots, 0)
	if m.state.KnownSessions[100] {
		t.Error("slots should stay unknown after a failed notification")
	}

	fail = false
	m.checkWeekendHappyHours(slots, 0)
	if calls != 2 || !m.state.KnownSessions[100] {
		t.Errorf("expected retry to succeed and mark slots known, calls=%d", calls)
	}
}

func TestCheckWeekendHappyHoursBooksEarliestSlot(t *testing.T) {
	var messages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		messages = append(messages, r.Header.Get("Title")+"|"+string(body))
	}))
	defer server.Close()

	m := newTestMonitor(NewNtfyNotifier(server.URL, ""))
	var booked []string
	m.bookHappyHour = func(code string) error {
		booked = append(booked, code)
		return nil
	}
	slots := happyHourSlots(upcomingDay(), 100, -1)
	slots[0].CapacityStatus = "FULL"

	m.checkWeekendHappyHours(slots, 3)
	if len(booked) != 1 || booked[0] != "EVENT_101" {
		t.Fatalf("expected the earliest slot with space to be booked, got %v", booked)
	}
	if len(messages) != 1 || !strings.Contains(messages[0], "booked") || !strings.Contains(messages[0], "Happy Hours left: 2") {
		t.Errorf("expected a booking notification, got %v", messages)
	}

	// Not rebooked, even if cancelled in the app
	m.checkWeekendHappyHours(slots, 2)
	if len(booked) != 1 {
		t.Errorf("expected no further bookings, got %v", booked)
	}
}

func TestCheckWeekendHappyHoursBookingLimits(t *testing.T) {
	m := newTestMonitor(nil)
	saturday := upcomingDay()
	slots := append(happyHourSlots(saturday, 100, -1), happyHourSlots(saturday.AddDate(0, 0, 1), 200, 0)...)
	ukTime := func(i int) string { return slots[i].StartAt.In(ukLocation).Format("15:04") }

	var booked []string
	m.bookHappyHour = func(code string) error {
		if code == "EVENT_101" {
			return fmt.Errorf("slot full")
		}
		booked = append(booked, code)
		return nil
	}
	// Prefer the last slot, then the second (which fails to book)
	m.SetHappyHourBooking(2, []string{ukTime(3), ukTime(1)})

	// Saturday gets the preferred slot, then the earliest one left.
	// Sunday already has one booked, so its last Happy Hour goes on a preferred slot.
	m.checkWeekendHappyHours(slots, 3)
	want := []string{"EVENT_103", "EVENT_100", "EVENT_203"}
	if strings.Join(booked, ",") != strings.Join(want, ",") {
		t.Errorf("expected bookings %v, got %v", want, booked)
	}
}

func TestCheckWeekendHappyHoursNoTokens(t *testing.T) {
	m := newTestMonitor(nil)
	m.bookHappyHour = func(code string) error {
		t.Errorf("unexpected booking of %s", code)
		return nil
	}
	m.checkWeekendHappyHours(happyHourSlots(upcomingDay(), 100, -1), 0)

	m.SetHappyHourBooking(0, nil)
	m.checkWeekendHappyHours(happyHourSlots(upcomingDay().AddDate(0, 0, 1), 200, -1), 5)
}

func TestNotifyWithoutNotifier(t *testing.T) {
	m := newTestMonitor(nil)
	if err := m.notify(NtfyMessage{Title: "x"}); err != nil {
		t.Errorf("expected no error without a notifier, got %v", err)
	}
}
