package tchannel

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uber/tchannel-go/testutils/goroutines"
)

func TestCheckedFramePoolForTest(t *testing.T) {
	tests := []struct {
		msg                string
		operations         func(pool *CheckedFramePoolForTest)
		wantHasIssues      bool
		wantBadAllocations int
		wantBadReleases    int
	}{
		{
			msg: "no bad releases or leaks",
			operations: func(pool *CheckedFramePoolForTest) {
				for i := 0; i < 10; i++ {
					pool.Release(pool.Get())
				}
			},
		},
		{
			msg: "frames are leaked",
			operations: func(pool *CheckedFramePoolForTest) {
				for i := 0; i < 10; i++ {
					pool.Release(pool.Get())
				}
				for i := 0; i < 10; i++ {
					_ = pool.Get()
				}
			},
			wantHasIssues:      true,
			wantBadAllocations: 10,
		},
		{
			msg: "frames are double released",
			operations: func(pool *CheckedFramePoolForTest) {
				for i := 0; i < 10; i++ {
					pool.Release(pool.Get())
				}
				f := pool.Get()
				pool.Release(f)
				pool.Release(f)
			},
			wantHasIssues:   true,
			wantBadReleases: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			pool := NewCheckedFramePoolForTest()

			tt.operations(pool)
			results := pool.CheckEmpty()

			assert.Equal(t, tt.wantHasIssues, results.HasIssues(), "Unexpected HasIssues() state")
			assert.Equal(t, tt.wantBadAllocations, len(results.Unreleased), "Unexpected allocs")
			assert.Equal(t, tt.wantBadReleases, len(results.BadReleases), "Unexpected bad releases")
		})
	}
}

func CheckFramePoolIsEmpty(t testing.TB, pool *CheckedFramePoolForTest) {
	t.Helper()

	stacks := goroutines.GetAll()
	if result := pool.CheckEmpty(); result.HasIssues() {
		if len(result.Unreleased) > 0 {
			t.Errorf("Frame pool has %v unreleased frames, errors:\n%v\nStacks:%v",
				len(result.Unreleased), strings.Join(result.Unreleased, "\n"), stacks)
		}
		if len(result.BadReleases) > 0 {
			t.Errorf("Frame pool has %v bad releases, errors:\n%v\nStacks:%v",
				len(result.BadReleases), strings.Join(result.BadReleases, "\n"), stacks)
		}
	}
}
