package agenda

import (
	"testing"
	"time"
)

func TestParseCronFiveAndSix(t *testing.T) {
	five, err := ParseCron("*/15 9-17 * * MON-FRI")
	if err != nil {
		t.Fatal(err)
	}
	if five.HasSecs {
		t.Fatal("five fields must not set seconds")
	}
	if !five.Minute[0] || !five.Minute[15] || five.Hour[8] || !five.Hour[17] {
		t.Fatal("step/range parse wrong")
	}
	if !five.Dow[int(time.Monday)] || five.Dow[int(time.Saturday)] {
		t.Fatal("dow names wrong")
	}
	six, err := ParseCron("30 0 12 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if !six.HasSecs || !six.Second[30] {
		t.Fatal("six fields must parse seconds")
	}
	for _, bad := range []string{"* * * *", "* * * * * * *", "61 * * * *", "x * * * *", "*/0 * * * *", "5-2 * * * *"} {
		if _, err := ParseCron(bad); err == nil {
			t.Fatalf("want error for %q", bad)
		}
	}
}

func TestCronNextBasic(t *testing.T) {
	spec, err := ParseCron("0 9 * * *")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.January, 5, 8, 0, 0, 0, time.UTC) // a Monday
	next, err := spec.Next(base, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.January, 5, 9, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("want %v, have %v", want, next)
	}
	// Strictly after: same minute does not refire.
	if _, err := ParseCron("0 9 * * *"); err != nil {
		t.Fatal(err)
	}
	next2, err := spec.Next(want, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if !next2.Equal(want.Add(24 * time.Hour)) {
		t.Fatalf("want next day, have %v", next2)
	}
}

func TestCronDaySemantics(t *testing.T) {
	// dom=13 and dow=FRI: either matches (standard cron OR).
	spec, err := ParseCron("0 0 13 * 5")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-02-13 is a Friday and the 13th: matches.
	friday13 := time.Date(2026, time.February, 13, 0, 0, 0, 0, time.UTC)
	if !spec.Matches(friday13) {
		t.Fatal("Friday 13th should match dom=13,dow=FRI")
	}
	// 2026-02-14 is a Saturday, not the 13th: no match.
	if spec.Matches(time.Date(2026, time.February, 14, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("Saturday 14th must not match")
	}
	// dom restricted, dow star: only dom decides.
	only, err := ParseCron("0 0 13 * *")
	if err != nil {
		t.Fatal(err)
	}
	if only.Matches(time.Date(2026, time.February, 14, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("14th must not match dom=13")
	}
}

func TestCronImpossible(t *testing.T) {
	spec, err := ParseCron("0 0 30 2 *")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := spec.Next(base, time.UTC); err == nil {
		t.Fatal("February 30th must never fire")
	}
}

func TestCronSixFieldSeconds(t *testing.T) {
	spec, err := ParseCron("*/20 * * * * *")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.January, 1, 0, 0, 5, 0, time.UTC)
	next, err := spec.Next(base, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if next.Second() != 20 {
		t.Fatalf("want second 20, have %v", next)
	}
}
