//go:build unit

package repository

import (
	"math"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestPaginateSliceLargePage(t *testing.T) {
	t.Parallel()
	for _, size := range []int{0, 1, 2, 3, 20, 21, 1000, 1001} {
		params := pagination.PaginationParams{Page: math.MaxInt, PageSize: size}
		require.Empty(t, paginateSlice([]int{1}, params), "page_size=%d", size)
	}
}
