# なぜこの形なのか

このパターンは Free monad を念頭に考えたもの。ならば枠組み自体を型として表現できるのではないか、Go のボイラープレートは型システムの弱さによるものではないか、を検討した記録。

結論だけ先に書くと、枠組みは Go でも型にできる。ただしそうすると 4 つ失う。現在の書き方はそれを避けた形であって、Free monad を書けなかった結果の妥協ではない。

## Go で枠組みを型にできるか

できる。`operational` monad の骨組みは Go の generics で表現できた。

```go
// Request[B] は「B が得られる要求」
type Request[B any] interface{ isRequestOf(B) }

// Program[A] は「最終的に A を返す計算の記述」
type Program[A any] interface{ isProgram(A) }

// 値をそのまま返す終端
type Pure[A any] struct{ Value A }

// 要求 1 つと、受け取った値で続きを作る関数。
// 結果の型 B は Continue のクロージャに閉じ込めるので、この型には現れない。
// Go には存在型が無いので、隠すにはこうするしかない。
type step[A any] struct {
	Request  any
	Continue func(any) Program[A]
}

// 型付きの要求と継続を受け取り、any へのキャストをここだけに閉じ込める
func Bind[A, B any](req Request[B], k func(B) Program[A]) Program[A] {
	return step[A]{
		Request:  req,
		Continue: func(v any) Program[A] { return k(v.(B)) },
	}
}
```

要求は結果の型を宣言する。

```go
type NeedArticle struct{ ArticleID ArticleID }
func (NeedArticle) isRequestOf(Article) {}

type NeedSuspended struct{ AuthorID AuthorID }
func (NeedSuspended) isRequestOf(bool) {}
```

判断は事実の struct が要らなくなる。確定した値は変数として積み上がる。

```go
func Publish(id ArticleID, now time.Time) Program[Result] {
	return Bind[Result](NeedArticle{ArticleID: id}, func(article Article) Program[Result] {
		if article.Status != "DRAFT" { return Pure[Result]{Value: Result{Err: errNotDraft}} }
		if article.Body == "" { return Pure[Result]{Value: Result{Err: errEmptyBody}} }
		return Bind[Result](NeedSuspended{AuthorID: article.AuthorID}, func(suspended bool) Program[Result] {
			if suspended { return Pure[Result]{Value: Result{Err: errSuspended}} }
			return Pure[Result]{Value: Result{ArticleID: article.ID, PublishedAt: now}}
		})
	})
}
```

要求と継続の型の対応はコンパイラが見る。`NeedArticle` に `func(b bool)` を渡すと弾かれる。

```
type func(b bool) Program[Result] does not match inferred type func(Article) Program[Result]
```

状態の型、マーカーメソッド、事実の struct、interpreter の分岐がすべて消える。

## 何を失うか

### 1. handler 側の型が守られない

interpreter に渡す handler は `func(req any) (any, error)` にならざるを得ない。「`NeedArticle` には `Article` を返す」という対応を Go の型で書く手段が無いため。間違えると実行時に落ちる。

```
panic: handler returned string, want Article
```

現在の書き方ではここが型で守られている。interpreter が `d.Decide(article)` と書く時点で `article` の型は静的に検査される。この一点が最大の差になる。

### 2. do 記法が無いので入れ子が深くなる

Haskell の Free monad が読みやすいのは do 記法があるから。Go には無いので、段が増えるとクロージャがその数だけ入れ子になる。Go のメソッドは型引数を持てないため `p.Bind(...)` と繋ぐこともできず、関数呼び出しの入れ子にしかならない。

### 3. 経路の静的列挙ができない

`Continue` はクロージャ。次にどの要求が出るかは値に依存して実行時に決まるので、外から覗けない。`flow` に相当するものは原理的に作れない。

### 4. 存在型が無い代償

`step` が `Request any` を持たざるを得ないのは Go に存在型が無いから。`Bind` の中に閉じ込めたので利用者からは見えないが、interpreter の switch で `any` に戻る。

## 他の言語ではどうなるか

同じ `Publish`（2 状態・4 終端）で比べる。

### TypeScript

判別可能なユニオンが言語機能なのでマーカーが要らない。さらに、状態と入力の対応を型のマップで書くと interpreter の分岐そのものが消える。

```ts
type Steps = {
  needArticle: { state: { articleId: ArticleId }; input: Article }
  needAuthorSuspension: { state: FactsArticle; input: { suspended: boolean; now: Date } }
}

type Need = { [K in keyof Steps]: { kind: K } & Steps[K]["state"] }[keyof Steps]
type StateOf<K extends keyof Steps> = Extract<Need, { kind: K }>

const decide: { [K in keyof Steps]: (s: StateOf<K>, input: Steps[K]["input"]) => Decision } = { ... }
type Fetchers = { [K in keyof Steps]: (s: StateOf<K>) => Promise<Steps[K]["input"]> }

// 状態が増えても伸びない
async function step<K extends keyof Steps>(s: StateOf<K>, f: Fetchers): Promise<Decision> {
  const k = s.kind as K
  return decide[k](s, await f[k](s))
}
```

`tsc --strict` で通ることと、次の 2 つがコンパイラだけで検出されることを確認した。外部 lint は要らない。

- `Steps` に状態を足して `decide` を埋め忘れる → `Property 'needQuota' is missing`
- 終端を足して interpreter の判定を直し忘れる → `not assignable to StateOf<keyof Steps>`

Go 版が空行とコメントを除いて 69 行（うちドメインの型が 20 行）、interpreter が別途状態数ぶん増えるのに対し、TypeScript は interpreter 込みで 37 行。しかも interpreter は状態が増えても伸びない。

### Rust

enum と `match` でマーカーと外部 lint が消える。ただし enum の variant ごとに入力の型を変えられないため、状態は結局 struct に分けることになる。

```rust
pub struct NeedArticle { pub article_id: ArticleId }
pub struct NeedAuthorSuspension { pub facts: FactsArticle }

pub enum Decision {
    NeedArticle(NeedArticle),
    NeedAuthorSuspension(NeedAuthorSuspension),
    Decided { facts: FactsArticle, published_at: DateTime<Utc> },
    Failed(PublishError),
}

impl NeedArticle {
    pub fn decide(self, article: Article) -> Decision { ... }
}
```

消えるのはマーカーメソッド、網羅チェックの外部ツール、`default:` の保険、nil を返せる穴。残るのは状態ごとの struct・`decide`・`match` の腕。埋め込みのフィールド昇格が無いので `s.facts.article_id` と書く必要があり、そこは Go より少し冗長になる。

構造としては Go に最も近く、削れるのは Go の型システム由来の分だけ。

### Haskell

ここだけ質が変わる。GADT と operational monad を使うと状態の型も事実の struct も消える。

```haskell
data Step a where
  NeedArticle    :: ArticleId -> Step Article
  NeedSuspension :: AuthorId  -> Step Bool

publish :: ArticleId -> Program Step (Either PublishError UTCTime)
publish aid = do
  article <- singleton (NeedArticle aid)
  if status article /= Draft then pure (Left NotDraft)
  else if null (body article) then pure (Left EmptyBody)
  else do
    suspended <- singleton (NeedSuspension (authorId article))
    if suspended then pure (Left AuthorSuspended) else pure (Right ...)

runIO :: Program Step a -> IO a
runIO = interpretWithMonad $ \case
  NeedArticle aid  -> fetchArticle aid
  NeedSuspension a -> fetchSuspended a
```

GADT により要求ごとに結果の型が変わり、handler 側も型で守られる。Go 版で `any` に落ちた部分がここでは落ちない。

事実の struct が要らないのが大きい。`article` はレキシカルスコープの変数で、取得前に使うことは型として不可能。Go が事実の struct と入れ子と `exhaustruct` で守っている性質が、ただの変数束縛で得られる。

tagless final ならさらに短い。Purity は型クラス制約が保証する。

```haskell
class Monad m => MonadPublish m where
  getArticle  :: ArticleId -> m Article
  isSuspended :: AuthorId  -> m Bool

publish :: MonadPublish m => ArticleId -> m (Either PublishError UTCTime)
```

ただし継続が関数である以上、経路の静的列挙はできない。Go の `Program` 版と同じ制約を負う。

## まとめ

| | マーカー | 網羅検査 | interpreter の分岐 | 事実の struct | handler の型 | 経路の静的列挙 |
|---|---|---|---|---|---|---|
| Go（現在の書き方） | 要る | 外部ツール | 状態ごと | 要る | 型で保証 | 可能 |
| Go（Program 版） | 不要 | — | 不要 | 不要 | `any` | 不可 |
| TypeScript | 不要 | コンパイラ | 不要 | 要る（型のみ） | 型で保証 | 可能 |
| Rust | 不要 | コンパイラ | 状態ごと | 要る | 型で保証 | 可能 |
| Haskell (GADT) | 不要 | コンパイラ | 命令ごと | 不要 | 型で保証 | 不可 |

Go のボイラープレートの内訳は 2 種類に分かれる。

- 型システム由来: マーカーメソッド（状態ごとに 1 行）、`go-check-sumtype` という外部ツール、`default:` の保険。sum type と網羅検査を持つ言語では消える
- エンコーディング由来: 状態ごとの型、状態ごとの `Decide`、interpreter の分岐。Rust でも残る

差の大部分は前者に集約される。状態を型として書くこと自体は Rust でも変わらない。

## 現在の形を選ぶ理由

現在の書き方は、継続を関数ではなく「名前の付いた状態型」に展開した形、いわゆる defunctionalization になっている。冗長さの一部は、構造を関数ではなくデータとして書き出したことの対価であって、Go の弱さだけが理由ではない。

Go では 2 つの言語機能の欠落が効く。存在型が無いので handler の型対応が `any` に落ち、do 記法が無いので段数ぶん入れ子が深くなる。defunctionalized な書き方はこの 2 つを回避し、副産物として経路の静的列挙を可能にしている。

逆に、段が 2〜3 で済み、静的解析も要らない場面なら `Program` 版のほうが素直になる。

## 検討したが採らなかったもの

goroutine とチャネルで do 記法に相当する平坦な記述を作る案がある。domain の関数を goroutine で動かし、要求をチャネルに送って応答を待つ形にすれば、入れ子は消えて straight-line に書ける。

採らない理由は 3 つ。domain 関数がチャネルのハンドルを持つので依存が戻ってくること、経路の静的列挙が失われること、goroutine の寿命管理と panic の伝播という別種の難しさが加わること。

## 今後の余地

型システム由来のコストは生成で削れる可能性がある。マーカーメソッドと interpreter の骨組みは状態の struct 定義から機械的に導ける。`//decision:decl` で対象を宣言し、状態を静的に列挙できているので必要な情報は揃っている。判断そのもの（`Decide` の中身）は人が書く部分なので触らない。
