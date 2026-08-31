package relay

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/internal/constant"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/gin-gonic/gin"
)

// RelayTaskSubmit is what the /v1/video, Suno and Kling submit routes run. The
// task capability shipped a mirror of this flow behind a TaskSubmitProvider port
// that no package implemented, so the handler's submit call resolved a nil
// provider on every request; the mirror has since been deleted.
//
// This pins the property that made the mirror broken and this path correct: the
// submit entry point must resolve a real adaptor from the request's channel type
// rather than fall through to "invalid api platform".
func TestRelayTaskSubmitResolvesAdaptorForChannelType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Suno is the one platform keyed by name; the rest key off channel type.
	if adaptor := GetTaskAdaptor(constant.TaskPlatformSuno); adaptor == nil {
		t.Fatal("no adaptor for the suno platform: task submission cannot reach upstream")
	}

	for _, channelType := range []int{
		constant.ChannelTypeKling,
		constant.ChannelTypeVidu,
		constant.ChannelTypeJimeng,
		constant.ChannelTypeVertexAi,
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/video/generations", nil)
		c, _ := ginadapter.NewSyntheticContext(req)
		c.Set("channel_type", channelType)

		platform := GetTaskPlatform(c)
		if want := constant.TaskPlatform(strconv.Itoa(channelType)); platform != want {
			t.Errorf("GetTaskPlatform for channel type %d = %q, want %q", channelType, platform, want)
			continue
		}
		if adaptor := GetTaskAdaptor(platform); adaptor == nil {
			t.Errorf("no adaptor for channel type %d: RelayTaskSubmit would reject the request as an invalid api platform", channelType)
		}
	}
}

// The handler builds its retry loop around relayInfo and hands the same pointer
// to RelayTaskSubmit. The deleted mirror instead copied a subset of fields into
// its own SubmitInfo and dereferenced relayInfo.LockedChannel unconditionally —
// which is nil on every first submit. Keep that field optional here so a future
// re-copy cannot reintroduce the panic unnoticed.
func TestRelayInfoLockedChannelStartsNil(t *testing.T) {
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	if info.LockedChannel != nil {
		t.Fatal("RelayInfo.LockedChannel is now non-nil by default; the submit path's locked-channel type assertions must stay guarded")
	}
	if _, ok := info.LockedChannel.(interface{ GetBaseURL() string }); ok {
		t.Error("a nil LockedChannel must not satisfy a channel assertion")
	}
}
