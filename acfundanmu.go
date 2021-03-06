package acfundanmu

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/Workiva/go-datastructures/queue"
)

// 队列长度
const queueLen = 1000

// ErrInitialize 就是初始化错误
//var ErrInitialize = errors.New("获取token失败，主播可能不在直播")

// DanmuType 就是弹幕信息的类型
type DanmuType int

const (
	// Comment 弹幕
	Comment DanmuType = iota
	// Like 点赞
	Like
	// EnterRoom 用户进入直播间
	EnterRoom
	// FollowAuthor 用户关注了主播
	FollowAuthor
	// ThrowBanana 投蕉
	ThrowBanana
	// Gift 礼物
	Gift
)

// Giftdetail 就是礼物的详细信息
type Giftdetail struct {
	ID            int    // 礼物ID
	Name          string // 礼物名字
	ARLiveName    string // 不为空时礼物属于虚拟偶像区的特殊礼物
	PayWalletType int    // 1为非免费礼物，2为免费礼物
	Price         int    // 礼物价格，非免费礼物时单位为ac币，免费礼物（香蕉）时为1
	WebpPic       string // 礼物的webp格式图片（动图）
	PngPic        string // 礼物的png格式图片（大）
	SmallPngPic   string // 礼物的png格式图片（小）
	CanCombo      bool   // 是否能连击，一般免费礼物（香蕉）不能连击，其余能连击
	MagicFaceID   int
	Description   string // 礼物的描述
	RedpackPrice  int    // 礼物红包价格总额，单位为AC币
}

// GiftInfo 就是弹幕里的礼物信息
type GiftInfo struct {
	Giftdetail                   // 礼物详细信息
	Count                 int    // 礼物数量
	Combo                 int    // 礼物连击数量
	Value                 int    // 礼物价值，非免费礼物时单位为ac币*1000，免费礼物（香蕉）时单位为礼物数量
	ComboID               string // 礼物连击ID
	SlotDisplayDurationMs int    // 应该是礼物动画持续的时间，送礼物后在该时间内再送一次可以实现礼物连击
	ExpireDurationMs      int
}

// UserInfo 就是用户信息
type UserInfo struct {
	UserID   int64     // 用户uid
	Nickname string    // 用户名字
	Avatar   string    // 用户头像
	Medal    MedalInfo // 粉丝牌
}

// MedalInfo 就是粉丝牌信息
type MedalInfo struct {
	UperID   int64  // UP主的uid
	ClubName string // 粉丝牌名字
	Level    int    // 粉丝牌等级
}

// DanmuMessage 就是websocket接受到的弹幕相关信息。
// 不论是哪种Type都会有SendTime、UserInfo里的UserID和Nickname，除了ThrowBanana没有UserInfo里的Avatar，其他Type通常都有Avatar，粉丝牌需要用户佩戴才有。
// Type为Comment时，Comment就是弹幕文字。
// Type为Gift时，Gift就是礼物信息。
// Type为Like、EnterRoom和FollowAuthor时没有多余的数据。
// Type为ThrowBanana时，BananaCount就是投蕉数量，不过现在基本是用Gift代替。
type DanmuMessage struct {
	Type        DanmuType // 弹幕类型
	SendTime    int64     // 弹幕发送时间，是以纳秒为单位的Unix时间
	UserInfo              // 用户信息
	Comment     string    // 弹幕内容
	BananaCount int       // 投蕉数量，现在基本上不用这个
	Gift        GiftInfo  // 礼物信息
}

// WatchingUser 就是观看直播的用户的信息，目前没有粉丝牌信息
type WatchingUser struct {
	UserInfo                      // 用户信息
	AnonymousUser          bool   // 是否匿名用户
	DisplaySendAmount      string // 赠送的全部礼物的价值，单位是ac币
	CustomWatchingListData string // 用户的一些额外信息，格式为json
}

// TopUser 就是礼物榜在线前三，目前没有粉丝牌信息。
// AnonymousUser好像通常为false，是否匿名用户需要根据UserID的大小来判断。
// DisplaySendAmount好像通常为空。
type TopUser WatchingUser

// LiveInfo 就是直播间的相关状态信息
type LiveInfo struct {
	KickedOut      string         // 被踢理由？
	ViolationAlert string         // 直播间警告？
	AllBananaCount string         // 直播间香蕉总数
	WatchingCount  string         // 直播间在线观众数量
	LikeCount      string         // 直播间点赞总数
	LikeDelta      int            // 点赞增加数量？
	TopUsers       []TopUser      // 礼物榜在线前三
	RecentComment  []DanmuMessage // 进直播间时显示的最近发的弹幕
}

// 带锁的LiveInfo
type liveInfo struct {
	sync.Mutex // LiveInfo的锁
	LiveInfo
}

// DanmuQueue 就是弹幕的队列
type DanmuQueue struct {
	q    *queue.Queue // DanmuMessage的队列
	info *liveInfo    // 直播间的相关信息状态
	t    *token       // 令牌相关信息
	ch   chan error   // 用来传递初始化的错误
}

// Login 用帐号邮箱和密码登陆AcFun获取cookies
func Login(username, password string) ([]*http.Cookie, error) {
	if username != "" && password != "" {
		return login(username, password)
	}
	return nil, fmt.Errorf("AcFun帐号邮箱或密码为空，无法登陆")
}

// Start 启动websocket获取弹幕，uid是主播的uid，ctx用来结束websocket。
// cookies可以利用Login()获取，为nil时使用访客模式登陆AcFun的弹幕系统，通常使用访客模式即可。
func Start(ctx context.Context, uid int, cookies []*http.Cookie) (dq *DanmuQueue, err error) {
	dq = new(DanmuQueue)
	dq.q = queue.New(queueLen)
	dq.info = new(liveInfo)
	dq.ch = make(chan error, 1)
	go dq.wsStart(ctx, uid, cookies)
	if err = <-dq.ch; err != nil {
		return nil, err
	}
	return dq, nil
}

// GetDanmu 返回弹幕数据danmu，danmu为nil时说明弹幕获取结束（出现错误或者主播下播）
func (dq *DanmuQueue) GetDanmu() (danmu []DanmuMessage) {
	if (*queue.Queue)(dq.q).Disposed() {
		return nil
	}
	ds, err := (*queue.Queue)(dq.q).Get(queueLen)
	if err != nil {
		return nil
	}

	danmu = make([]DanmuMessage, len(ds))
	for i, d := range ds {
		danmu[i] = d.(DanmuMessage)
	}

	return danmu
}

// GetInfo 返回直播间的状态信息info
func (dq *DanmuQueue) GetInfo() (info LiveInfo) {
	dq.info.Lock()
	defer dq.info.Unlock()
	info = dq.info.LiveInfo
	return info
}

// GetWatchingList 返回直播间排名前50的在线观众信息列表
func (dq *DanmuQueue) GetWatchingList(cookies []*http.Cookie) *[]WatchingUser {
	watchList, err := dq.t.watchingList(cookies)
	checkErr(err)
	return watchList
}
