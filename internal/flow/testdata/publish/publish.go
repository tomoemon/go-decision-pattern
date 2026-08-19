// Package publish は README に載せる出力例。記事の公開可否を判定する。
//
// 規約どおりに書いた最小構成で、テストからも解析するので README の図が古くならない。
package publish

import (
	"errors"
	"time"
)

type (
	ArticleID string
	AuthorID  string
	Status    string
)

const (
	StatusDraft     Status = "DRAFT"
	StatusPublished Status = "PUBLISHED"
)

type Article struct {
	ID       ArticleID
	AuthorID AuthorID
	Status   Status
	Body     string
}

var (
	ErrNotDraft        = errors.New("article is not a draft")
	ErrEmptyBody       = errors.New("article body is empty")
	ErrAuthorSuspended = errors.New("author is suspended")
)

// PublishDecision は記事の公開可否の判定。
//
//sumtype:decl
//decision:decl
type PublishDecision interface {
	isPublishDecision()
}

// publishFactsArticle は Article 取得で確定する事実。
type publishFactsArticle struct {
	ArticleID ArticleID
	AuthorID  AuthorID
}

// PublishDecided は公開してよいと決まった状態。
type PublishDecided struct {
	publishFactsArticle
	PublishedAt time.Time
}

func (PublishDecided) isPublishDecision() {}

// PublishFailed は公開できないと決まった状態。
type PublishFailed struct {
	Err error
}

func (PublishFailed) isPublishDecision() {}

// NewPublishDecision は判定を開始する。
func NewPublishDecision(id ArticleID) PublishDecision {
	return PublishNeedArticle{ArticleID: id}
}

// PublishNeedArticle は記事本体が必要な状態。まだ何も確定していないので取得キーだけを持つ。
type PublishNeedArticle struct {
	ArticleID ArticleID
}

func (PublishNeedArticle) isPublishDecision() {}

// Decide は下書きで本文があるときだけ、著者の停止状態の確認へ進む。
func (s PublishNeedArticle) Decide(article Article) PublishDecision {
	if article.Status != StatusDraft {
		return PublishFailed{Err: ErrNotDraft}
	}
	if article.Body == "" {
		return PublishFailed{Err: ErrEmptyBody}
	}
	return PublishNeedAuthorSuspension{
		publishFactsArticle: publishFactsArticle{ArticleID: article.ID, AuthorID: article.AuthorID},
	}
}

// PublishNeedAuthorSuspension は著者が停止中かどうかが必要な状態。
// 取得キーは確定済みの AuthorID。
type PublishNeedAuthorSuspension struct {
	publishFactsArticle
}

func (PublishNeedAuthorSuspension) isPublishDecision() {}

// Decide は著者が停止中でなければ公開してよいと決める。
func (s PublishNeedAuthorSuspension) Decide(suspended bool, now time.Time) PublishDecision {
	if suspended {
		return PublishFailed{Err: ErrAuthorSuspended}
	}
	return PublishDecided{publishFactsArticle: s.publishFactsArticle, PublishedAt: now}
}
