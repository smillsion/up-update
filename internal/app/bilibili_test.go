package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestParseMID(t *testing.T) {
	cases := map[string]string{"546195": "546195", "https://space.bilibili.com/546195/video": "546195"}
	for input, want := range cases {
		got, err := parseMID(input)
		if err != nil || got != want {
			t.Fatalf("parseMID(%q)=%q,%v", input, got, err)
		}
	}
	for _, input := range []string{"", "abc", "https://example.com/546195", "0"} {
		if _, err := parseMID(input); err == nil {
			t.Fatalf("parseMID(%q) should fail", input)
		}
	}
}

func TestSignWBIIsDeterministic(t *testing.T) {
	values := url.Values{"mid": {"546195"}, "keyword": {"a!b(c)"}}
	one := signWBI(values, "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png", "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png", 1720000000)
	two := signWBI(values, "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png", "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png", 1720000000)
	if one.Get("w_rid") != two.Get("w_rid") || len(one.Get("w_rid")) != 32 {
		t.Fatal("signature is not deterministic")
	}
	if one.Get("keyword") != "abc" || one.Get("wts") != "1720000000" {
		t.Fatalf("unexpected signed values: %v", one)
	}
}

func TestBilibiliClient(t *testing.T) {
	var navRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "SESSDATA=test" {
			t.Error("cookie missing")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/x/web-interface/nav":
			navRequests.Add(1)
			fmt.Fprint(w, `{"code":0,"data":{"isLogin":true,"mid":123,"uname":"tester","wbi_img":{"img_url":"https://i/a1234567890123456789012345678901.png","sub_url":"https://i/b1234567890123456789012345678901.png"}}}`)
		case "/x/web-interface/card":
			fmt.Fprint(w, `{"code":0,"data":{"card":{"mid":"546195","name":"测试UP","face":"https://image/avatar.jpg"}}}`)
		case "/x/space/wbi/arc/search":
			if r.URL.Query().Get("w_rid") == "" {
				t.Error("signature missing")
			}
			if r.URL.Query().Get("mid") == "999" {
				fmt.Fprint(w, `{"code":-509,"message":"limited"}`)
				return
			}
			fmt.Fprint(w, `{"code":0,"data":{"list":{"vlist":[{"bvid":"BV2","title":"新视频","created":200},{"bvid":"BV1","title":"旧视频","created":100}]}}}`)
		case "/x/relation/followings":
			if r.URL.Query().Get("vmid") != "123" || r.URL.Query().Get("pn") != "2" || r.URL.Query().Get("ps") != "50" {
				t.Errorf("unexpected following query: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"code":0,"data":{"list":[{"mid":456,"uname":"关注的UP","face":"http://image/following.jpg"}],"total":51}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewBilibiliClient(server.URL)
	ctx := context.Background()
	identity, err := client.ValidateCookie(ctx, "SESSDATA=test")
	if err != nil || identity.Name != "tester" {
		t.Fatalf("validate: %#v %v", identity, err)
	}
	creator, err := client.GetCreator(ctx, "546195", "SESSDATA=test")
	if err != nil || creator.Name != "测试UP" {
		t.Fatalf("creator: %#v %v", creator, err)
	}
	videos, err := client.GetLatestVideos(ctx, "546195", "SESSDATA=test")
	if err != nil || len(videos) != 2 || videos[0].BVID != "BV1" {
		t.Fatalf("videos: %#v %v", videos, err)
	}
	followings, err := client.GetFollowings(ctx, "SESSDATA=test", 2, 50)
	if err != nil || followings.Total != 51 || len(followings.Items) != 1 || followings.Items[0].MID != "456" || followings.Items[0].Avatar != "https://image/following.jpg" {
		t.Fatalf("followings: %#v %v", followings, err)
	}
	beforeBatch := navRequests.Load()
	batch := client.GetLatestVideosBatch(ctx, []string{"546195", "999"}, "SESSDATA=test", 2)
	if navRequests.Load()-beforeBatch != 1 {
		t.Fatalf("batch nav requests=%d, want 1", navRequests.Load()-beforeBatch)
	}
	if len(batch) != 2 || batch[0].Err != nil || len(batch[0].Videos) != 2 || batch[1].Err == nil {
		t.Fatalf("batch results: %#v", batch)
	}
}

func TestBilibiliClientSupportsAnonymousPublicQueries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/x/web-interface/nav":
			fmt.Fprint(w, `{"code":-101,"message":"账号未登录","data":{"isLogin":false,"wbi_img":{"img_url":"https://i/a1234567890123456789012345678901.png","sub_url":"https://i/b1234567890123456789012345678901.png"}}}`)
		case "/x/web-interface/card":
			if r.Header.Get("Cookie") != "" {
				t.Error("anonymous creator query included a cookie")
			}
			fmt.Fprint(w, `{"code":0,"data":{"card":{"mid":"546195","name":"测试UP","face":"https://image/avatar.jpg"}}}`)
		case "/x/space/wbi/arc/search":
			if r.Header.Get("Cookie") != "" {
				t.Error("anonymous video query included a cookie")
			}
			if r.URL.Query().Get("w_rid") == "" {
				t.Error("signature missing")
			}
			fmt.Fprint(w, `{"code":0,"data":{"list":{"vlist":[{"bvid":"BV1","title":"新视频","created":200}]}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewBilibiliClient(server.URL)
	creator, err := client.GetCreator(context.Background(), "546195", "")
	if err != nil || creator.Name != "测试UP" {
		t.Fatalf("creator: %#v %v", creator, err)
	}
	videos, err := client.GetLatestVideos(context.Background(), "546195", "")
	if err != nil || len(videos) != 1 || videos[0].BVID != "BV1" {
		t.Fatalf("videos: %#v %v", videos, err)
	}
	_, err = client.ValidateCookie(context.Background(), "SESSDATA=expired")
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || !providerErr.Auth {
		t.Fatalf("ValidateCookie error=%v, want auth error", err)
	}
}
