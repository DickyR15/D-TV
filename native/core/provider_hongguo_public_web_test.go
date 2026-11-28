package core

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	openhongguo "github.com/beilunyang/openhongguo/sdk"
)

const publicWebDetailFixture = `<script>window._ROUTER_DATA={"loaderData":{"detail_page":{"isSuccess":true,"seriesDetail":{"series_id":"7710822671759461438","episode_cnt":2,"accessible_episode_cnt":1,"vid_list":["7710822671759461439","7710822671759461440"]}}}};</script>`
const publicWebPlayerFixture = `<script>window._ROUTER_DATA={"loaderData":{"player_page":{"series_id":"7710822671759461438","vid":"7710822671759461439","video_player_info":{"main_url":"https://v.qznovelvod.com/episode.mp4","height":1080,"duration":90}}}};</script>`

func TestHongguoPublicWebEpisodeScope(t *testing.T) {
	const sid = "7710822671759461438"
	const vid = "7710822671759461439"
	if !hongguoPublicWebEpisodeAllowed(publicWebDetailFixture,sid,vid) {
		t.Fatal("explicit public episode was not recognized")
	}
	if hongguoPublicWebEpisodeAllowed(publicWebDetailFixture,sid,"7710822671759461440") {
		t.Fatal("non-public episode was accepted")
	}
	for _, body := range []string{
		strings.Replace(publicWebDetailFixture,`"accessible_episode_cnt":1`,`"accessible_episode_cnt":0`,1),
		strings.Replace(publicWebDetailFixture,`"accessible_episode_cnt":1,`,"",1),
		strings.Replace(publicWebDetailFixture,"7710822671759461440",vid,1),
		strings.Replace(publicWebDetailFixture,`"isSuccess":true`,`"isSuccess":false`,1),
	} {
		if hongguoPublicWebEpisodeAllowed(body,sid,vid) {
			t.Fatal("invalid public scope was accepted")
		}
	}
}

func TestHongguoPublicWebRejectsProtectedOrWrongEpisode(t *testing.T) {
	const sid = "7710822671759461438"
	const vid = "7710822671759461439"
	if media, err := hongguoPublicWebMediaFromPage(publicWebPlayerFixture,sid,vid); err != nil || media.Quality != 1080 {
		t.Fatalf("public media not resolved: %v",err)
	}
	for _, body := range []string{
		strings.Replace(publicWebPlayerFixture,vid,"7710822671759461440",1),
		strings.Replace(publicWebPlayerFixture,`"height":1080`,`"height":480`,1),
		strings.Replace(publicWebPlayerFixture,`"height":1080`,`"height":1080,"drm":true`,1),
		strings.Replace(publicWebPlayerFixture,`"height":1080`,`"height":1080,"preview":true`,1),
		strings.Replace(publicWebPlayerFixture,"v.qznovelvod.com","qznovelvod.com.example.test",1),
	} {
		if _, err := hongguoPublicWebMediaFromPage(body,sid,vid); err == nil {
			t.Fatal("unsafe or mismatched media was accepted")
		}
	}
}

func TestHongguoPublicWebFallbackDuringSDKPlaybackCooldown(t *testing.T) {
	var requests int
	d := sourceFixtureDownloader(t,func(req *http.Request)(*http.Response,error){
		requests++
		if req.URL.Hostname() != "hongguoduanju.com" {
			t.Fatalf("unexpected host: %s",req.URL.Hostname())
		}
		switch req.URL.Path {
		case "/detail":
			return sourceFixtureResponse(req,http.StatusOK,publicWebDetailFixture),nil
		case "/player/7710822671759461438/7710822671759461439":
			return sourceFixtureResponse(req,http.StatusOK,publicWebPlayerFixture),nil
		default:
			t.Fatalf("unexpected request: %s",req.URL.Path)
			return nil,errors.New("unexpected request")
		}
	})
	d.hongguoSDKSession.recordPlaybackResult(openhongguo.ErrAnonymousLimited)
	ctx := context.WithValue(context.Background(),hongguoPlaybackOriginKey{},"resolve")
	task := Task{DramaID:"hongguo:7710822671759461438", Chapter:Chapter{VideoURL:"hongguo-cenc://7710822671759461439"}}
	media, err := d.resolveHongguoMedia(ctx,task)
	if err != nil || media.Quality != 1080 || requests != 2 {
		t.Fatalf("public fallback failed: quality=%d requests=%d err=%v",media.Quality,requests,err)
	}
	task.Chapter.VideoURL = "hongguo-cenc://7710822671759461440"
	_, err = d.resolveHongguoMedia(ctx,task)
	if !errors.Is(err,errHongguoSDKPlaybackCooling) || requests != 3 {
		t.Fatalf("non-public episode must stop at detail: requests=%d err=%v",requests,err)
	}
}
