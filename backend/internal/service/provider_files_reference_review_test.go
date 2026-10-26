//go:build unit || !integration

package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type providerFilesReferenceReviewStore struct {
	ProviderFilesRepository
	file  *ProviderFile
	reads []string
}

func (s *providerFilesReferenceReviewStore) Get(_ context.Context, id string) (*ProviderFile, error) {
	s.reads = append(s.reads, id)
	if s.file != nil && id == s.file.PublicID {
		return s.file, nil
	}
	return nil, ErrProviderFileNotFound
}

func TestProviderFilesReviewEscapedIDsPreserveACLRewriteAndOwner(t *testing.T) {
	const localID = "file-s2a-11111111-1111-4111-8111-111111111111"
	const remoteID = "native-file-review"
	for _, platform := range []string{PlatformDeepseek, PlatformMiniMax} {
		for _, encoding := range []string{"hyphens", "first-letter"} {
			for _, access := range []string{"authorized", "other-key", "unknown"} {
				t.Run(platform+"/"+encoding+"/"+access, func(t *testing.T) {
					owner := &Account{ID: 11, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "review-owner-key", "api_protocol": APIProtocolAnthropic}}
					principal, err := providerFilePrincipal(owner)
					require.NoError(t, err)
					file := &ProviderFile{PublicID: localID, UserID: 2, APIKeyID: 3, GroupID: 4, Platform: platform, AccountID: owner.ID, Principal: principal, RemoteID: remoteID, State: ProviderFileReady}
					store := &providerFilesReferenceReviewStore{file: file}
					if access == "unknown" {
						store.file = nil
					}
					key := &APIKey{ID: 3, UserID: 2, GroupID: int64Ptr(4)}
					if access == "other-key" {
						key.ID = 99
					}
					wireID := strings.ReplaceAll(localID, "-", `\u002d`)
					if encoding == "first-letter" {
						wireID = `\u0066` + localID[1:]
					}
					body := []byte(fmt.Sprintf(`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"%s"}}]}]}`, wireID))
					path := "messages.0.content.0.source.file_id"
					wantRemote := remoteID
					if platform == PlatformMiniMax {
						body = []byte(fmt.Sprintf(`{"messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"mm_file://%s"}}]}]}`, wireID))
						path = "messages.0.content.0.video_url.url"
						wantRemote = "mm_file://" + remoteID
					}
					require.NotContains(t, string(body), ProviderFileIDPrefix, "the fixture uses a semantically identical JSON escape, without a literal namespace substring")
					svc := NewProviderFilesService(store, nil)
					ctx, rewritten, err := svc.ResolveReferences(context.Background(), key, platform, body)
					if access != "authorized" {
						require.ErrorIs(t, err, ErrProviderFileNotFound, "JSON spelling cannot bypass the exact API-key scope or turn an unknown gateway ID into a native reference")
						require.Nil(t, rewritten)
					} else {
						require.NoError(t, err)
						require.Equal(t, wantRemote, gjson.GetBytes(rewritten, path).String())
						require.True(t, providerFilesAccountAllowed(ctx, owner))
						other := *owner
						other.ID = 12
						require.False(t, providerFilesAccountAllowed(ctx, &other), "the decoded local reference must impose a hard credential-owner constraint")
					}
					require.Equal(t, []string{localID}, store.reads, "ACL lookup must use the decoded identifier")
				})
			}
		}
	}
}

func TestProviderFilesReviewMiniMaxRejectsNonVideoISOBrands(t *testing.T) {
	// The MP4 registration authority assigns avif/avis to AVIF and heic/heix
	// to HEIF, not the gateway's reviewed MP4/QuickTime upload family:
	// https://mp4ra.org/registered-types/brands
	for _, scenario := range []struct {
		name, major, compatible, mime string
		accepted                      bool
	}{
		{"mp4-isom", "isom", "mp42", "video/mp4", true},
		{"mp4-v1", "mp41", "isom", "video/mp4", true},
		{"mp4-v2", "mp42", "isom", "video/mp4", true},
		{"mov-quicktime", "qt  ", "qt  ", "video/quicktime", true},
		{"avif-image", "avif", "mif1", "video/mp4", false},
		{"avif-image-sequence", "avis", "msf1", "video/mp4", false},
		{"heic-image", "heic", "mif1", "video/mp4", false},
		{"heix-image", "heix", "mif1", "video/mp4", false},
		{"generic-major-avif-compatibility", "isom", "avif", "video/mp4", false},
		{"generic-major-heif-compatibility", "isom", "heic", "video/mp4", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// A well-formed 24-byte ftyp box plus an empty mdat box. Merely
			// sharing the ISO box signature cannot turn an image into MP4.
			body := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p'}, []byte(scenario.major)...)
			body = append(body, 0, 0, 0, 0)
			body = append(body, []byte(scenario.compatible)...)
			body = append(body, []byte(scenario.compatible)...)
			body = append(body, 0, 0, 0, 8, 'm', 'd', 'a', 't')
			file, err := ValidateProviderFileUpload(PlatformMiniMax, "review.mp4", scenario.mime, "video_understanding", 0, bytes.NewReader(body))
			if scenario.accepted {
				require.NoError(t, err)
				require.NotNil(t, file)
				require.Equal(t, scenario.mime, file.MIMEType)
			} else {
				require.Error(t, err, "image ftyp brands must be rejected before any video_understanding scheduling or provider upload")
				require.Nil(t, file)
			}
		})
	}
}
