//go:build unit || !integration

package service

import (
	"context"
	"sync/atomic"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
)

type projectIssuerStub struct {
	ZhipuOAuthClient
	calls atomic.Int32
	issue func(context.Context, string) (*zcode.ProjectAccessToken, error)
}

func (s *projectIssuerStub) IssueProjectAccessToken(ctx context.Context, _ string, login, _, _, _, _ string) (*zcode.ProjectAccessToken, error) {
	s.calls.Add(1)
	return s.issue(ctx, login)
}
