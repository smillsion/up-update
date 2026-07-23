package app

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ProviderError struct {
	Code      int
	Message   string
	Retryable bool
	Auth      bool
}

func (e *ProviderError) Error() string { return e.Message }

type BilibiliIdentity struct{ Name string }
type Creator struct {
	MID    string `json:"mid"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}
type Following struct {
	MID    string `json:"mid"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}
type FollowingPage struct {
	Items    []Following
	Page     int
	PageSize int
	Total    int
}
type Video struct {
	BVID        string `json:"bvid"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	PublishedAt int64  `json:"publishedAt"`
}
type VideoFetchResult struct {
	MID    string
	Videos []Video
	Err    error
}

type BilibiliClient struct {
	baseURL     string
	passportURL string
	wwwURL      string
	http        *http.Client
}

func NewBilibiliClient(baseURL string) *BilibiliClient {
	return &BilibiliClient{
		baseURL: strings.TrimRight(baseURL, "/"), passportURL: "https://passport.bilibili.com", wwwURL: "https://www.bilibili.com",
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

type biliEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *BilibiliClient) fetchEnvelope(ctx context.Context, endpoint string, query url.Values, cookie string) (biliEnvelope, error) {
	u := c.baseURL + endpoint
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return biliEnvelope{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/138.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://space.bilibili.com/")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return biliEnvelope{}, &ProviderError{Message: "连接 B 站失败", Retryable: true}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return biliEnvelope{}, err
	}
	if resp.StatusCode == http.StatusPreconditionFailed {
		return biliEnvelope{}, &ProviderError{Code: 412, Message: "B 站触发访问风控，请稍后重试", Retryable: true}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return biliEnvelope{}, &ProviderError{Code: resp.StatusCode, Message: fmt.Sprintf("B 站返回 HTTP %d", resp.StatusCode), Retryable: resp.StatusCode >= 500}
	}
	var envelope biliEnvelope
	if json.Unmarshal(body, &envelope) != nil {
		return biliEnvelope{}, errors.New("B 站返回了无法解析的数据")
	}
	return envelope, nil
}

func biliEnvelopeError(envelope biliEnvelope) error {
	if envelope.Code != 0 {
		auth := envelope.Code == -101 || envelope.Code == -400
		retry := envelope.Code == -352 || envelope.Code == -509
		return &ProviderError{Code: envelope.Code, Message: fmt.Sprintf("B 站接口错误：%s (%d)", envelope.Message, envelope.Code), Retryable: retry, Auth: auth}
	}
	return nil
}

func decodeBiliData(envelope biliEnvelope, destination any) error {
	if destination != nil {
		if err := json.Unmarshal(envelope.Data, destination); err != nil {
			return fmt.Errorf("解析 B 站数据: %w", err)
		}
	}
	return nil
}

func (c *BilibiliClient) request(ctx context.Context, endpoint string, query url.Values, cookie string, destination any) error {
	envelope, err := c.fetchEnvelope(ctx, endpoint, query, cookie)
	if err != nil {
		return err
	}
	if err := biliEnvelopeError(envelope); err != nil {
		return err
	}
	return decodeBiliData(envelope, destination)
}

type navData struct {
	IsLogin  bool   `json:"isLogin"`
	MID      int64  `json:"mid"`
	Uname    string `json:"uname"`
	WBIImage struct {
		ImageURL string `json:"img_url"`
		SubURL   string `json:"sub_url"`
	} `json:"wbi_img"`
}

func (c *BilibiliClient) nav(ctx context.Context, cookie string) (navData, error) {
	var data navData
	envelope, err := c.fetchEnvelope(ctx, "/x/web-interface/nav", nil, cookie)
	if err != nil {
		return data, err
	}
	if envelope.Code != 0 && !(strings.TrimSpace(cookie) == "" && envelope.Code == -101) {
		return data, biliEnvelopeError(envelope)
	}
	if err := decodeBiliData(envelope, &data); err != nil {
		return data, err
	}
	if strings.TrimSpace(cookie) == "" && (data.WBIImage.ImageURL == "" || data.WBIImage.SubURL == "") {
		return data, errors.New("无法获取 B 站请求签名")
	}
	return data, nil
}
func (c *BilibiliClient) ValidateCookie(ctx context.Context, cookie string) (BilibiliIdentity, error) {
	if strings.TrimSpace(cookie) == "" {
		return BilibiliIdentity{}, errors.New("Cookie 不能为空")
	}
	data, err := c.nav(ctx, cookie)
	if err != nil {
		return BilibiliIdentity{}, err
	}
	if !data.IsLogin {
		return BilibiliIdentity{}, &ProviderError{Code: -101, Message: "Cookie 未登录或已失效", Auth: true}
	}
	return BilibiliIdentity{Name: data.Uname}, nil
}

func (c *BilibiliClient) GetCreator(ctx context.Context, mid, cookie string) (Creator, error) {
	var data struct {
		Card struct {
			MID  string `json:"mid"`
			Name string `json:"name"`
			Face string `json:"face"`
		} `json:"card"`
	}
	err := c.request(ctx, "/x/web-interface/card", url.Values{"mid": {mid}}, cookie, &data)
	if err != nil {
		return Creator{}, err
	}
	if data.Card.MID == "" {
		return Creator{}, errors.New("没有找到这个 UP 主")
	}
	return Creator{MID: data.Card.MID, Name: data.Card.Name, Avatar: strings.Replace(data.Card.Face, "http://", "https://", 1)}, nil
}

func (c *BilibiliClient) GetFollowings(ctx context.Context, cookie string, page, pageSize int) (FollowingPage, error) {
	if page < 1 || pageSize < 1 || pageSize > 50 {
		return FollowingPage{}, errors.New("关注列表分页参数不正确")
	}
	nav, err := c.nav(ctx, cookie)
	if err != nil {
		return FollowingPage{}, err
	}
	if !nav.IsLogin || nav.MID <= 0 {
		return FollowingPage{}, &ProviderError{Code: -101, Message: "Cookie 未登录或已失效", Auth: true}
	}
	var data struct {
		List []struct {
			MID    int64  `json:"mid"`
			Name   string `json:"uname"`
			Avatar string `json:"face"`
		} `json:"list"`
		Total int `json:"total"`
	}
	query := url.Values{
		"vmid":  {strconv.FormatInt(nav.MID, 10)},
		"pn":    {strconv.Itoa(page)},
		"ps":    {strconv.Itoa(pageSize)},
		"order": {"desc"},
	}
	if err := c.request(ctx, "/x/relation/followings", query, cookie, &data); err != nil {
		return FollowingPage{}, err
	}
	items := make([]Following, 0, len(data.List))
	for _, item := range data.List {
		if item.MID <= 0 {
			continue
		}
		items = append(items, Following{
			MID:    strconv.FormatInt(item.MID, 10),
			Name:   item.Name,
			Avatar: strings.Replace(item.Avatar, "http://", "https://", 1),
		})
	}
	return FollowingPage{Items: items, Page: page, PageSize: pageSize, Total: data.Total}, nil
}

func (c *BilibiliClient) GetLatestVideos(ctx context.Context, mid, cookie string) ([]Video, error) {
	nav, err := c.nav(ctx, cookie)
	if err != nil {
		return nil, err
	}
	return c.getLatestVideos(ctx, mid, cookie, nav)
}

func (c *BilibiliClient) GetLatestVideosBatch(ctx context.Context, mids []string, cookie string, concurrency int) []VideoFetchResult {
	results := make([]VideoFetchResult, len(mids))
	for index, mid := range mids {
		results[index].MID = mid
	}
	if len(mids) == 0 {
		return results
	}
	nav, err := c.nav(ctx, cookie)
	if err != nil {
		for index := range results {
			results[index].Err = err
		}
		return results
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(mids) {
		concurrency = len(mids)
	}
	jobs := make(chan int, len(mids))
	for index := range mids {
		jobs <- index
	}
	close(jobs)
	var workers sync.WaitGroup
	workers.Add(concurrency)
	for range concurrency {
		go func() {
			defer workers.Done()
			for index := range jobs {
				if err := ctx.Err(); err != nil {
					results[index].Err = err
					continue
				}
				results[index].Videos, results[index].Err = c.getLatestVideos(ctx, mids[index], cookie, nav)
			}
		}()
	}
	workers.Wait()
	return results
}

func (c *BilibiliClient) getLatestVideos(ctx context.Context, mid, cookie string, nav navData) ([]Video, error) {
	if nav.WBIImage.ImageURL == "" || nav.WBIImage.SubURL == "" {
		return nil, errors.New("无法获取 B 站请求签名")
	}
	values := url.Values{"mid": {mid}, "pn": {"1"}, "ps": {"30"}, "order": {"pubdate"}, "platform": {"web"}, "web_location": {"1550101"}}
	signed := signWBI(values, nav.WBIImage.ImageURL, nav.WBIImage.SubURL, time.Now().Unix())
	var data struct {
		List struct {
			VList []struct {
				BVID    string `json:"bvid"`
				Title   string `json:"title"`
				Created int64  `json:"created"`
			} `json:"vlist"`
		} `json:"list"`
	}
	if err := c.request(ctx, "/x/space/wbi/arc/search", signed, cookie, &data); err != nil {
		return nil, err
	}
	videos := make([]Video, 0, len(data.List.VList))
	for _, item := range data.List.VList {
		if item.BVID != "" {
			videos = append(videos, Video{BVID: item.BVID, Title: item.Title, URL: "https://www.bilibili.com/video/" + item.BVID, PublishedAt: item.Created})
		}
	}
	sort.Slice(videos, func(i, j int) bool { return videos[i].PublishedAt < videos[j].PublishedAt })
	return videos, nil
}

var mixinIndexes = []int{46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13, 37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 30, 4, 22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11, 36, 20, 34, 44, 52}

func signWBI(values url.Values, imageURL, subURL string, timestamp int64) url.Values {
	keyPart := func(raw string) string {
		parsed, _ := url.Parse(raw)
		name := path.Base(parsed.Path)
		return strings.TrimSuffix(name, path.Ext(name))
	}
	original := keyPart(imageURL) + keyPart(subURL)
	var builder strings.Builder
	for _, index := range mixinIndexes {
		if index < len(original) && builder.Len() < 32 {
			builder.WriteByte(original[index])
		}
	}
	cleaned := url.Values{}
	for key, list := range values {
		for _, value := range list {
			cleaned.Add(key, strings.Map(func(r rune) rune {
				if strings.ContainsRune("!'()*", r) {
					return -1
				}
				return r
			}, value))
		}
	}
	cleaned.Set("wts", strconv.FormatInt(timestamp, 10))
	sum := md5.Sum([]byte(cleaned.Encode() + builder.String()))
	cleaned.Set("w_rid", hex.EncodeToString(sum[:]))
	return cleaned
}

func parseMID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("请输入 UID 或空间链接")
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		if !strings.HasSuffix(strings.ToLower(parsed.Host), "bilibili.com") {
			return "", errors.New("只支持 B 站空间链接")
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) > 0 {
			value = parts[0]
		}
	}
	mid, err := strconv.ParseUint(value, 10, 64)
	if err != nil || mid == 0 {
		return "", errors.New("UID 格式不正确")
	}
	return strconv.FormatUint(mid, 10), nil
}
