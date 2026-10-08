// TestDescribeSceneAge: checks scene ages are phrased in minutes, hours or days as appropriate.

package cdse

import (
	"testing"
	"time"
)

func TestDescribeSceneAge(t *testing.T) {
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Minute, "under an hour ago"},
		{5*time.Hour + time.Minute, "5 hours ago"},
		{72*time.Hour + time.Minute, "3 days ago"},
	}
	for _, c := range cases {
		if got := DescribeSceneAge(time.Now().Add(-c.ago)); got != c.want {
			t.Errorf("%s ago read as %q, want %q", c.ago, got, c.want)
		}
	}
}
