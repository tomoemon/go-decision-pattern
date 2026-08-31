# Decision パターン

規約バージョン: v0.1.1 — 最新は https://github.com/tomoemon/go-decision-pattern/releases で確認する

Free Monad / Interpreter パターンに近い概念で、「計算の記述」と「計算の実行」を分離する。DDD トリレンマ（Purity, Completeness, Performance）を解決するためのパターン。

- Purity: domain の Decide メソッドは純粋関数。副作用（DB アクセス等）を持たない
- Completeness: 次に何を取るかという判断まで domain が持つので、ドメインロジックが呼び出し側に分断されない
- Performance: interpreter が必要なデータだけを逐次取得する

この文書は domain の外側で取得を行う層を「interpreter」と呼ぶ。層の名前はリポジトリによって異なる（usecase / application など）。

## 適用基準

判定条件は「domain の判断結果によって、次に取得するものが変わるか」。エンティティの種類が変わる場合だけでなく、同じ種類を別のキーで取る場合や、そもそも取得するかどうかが変わる場合も含む。変わるならトリレンマが発生しているので Decision パターンを検討する。

変わらないなら不要。取得が何段階に分かれていても、取得するものが判断結果に依存しないなら、interpreter 側ですべて取得して domain の純粋関数に渡せば完結する。「取得と判断の呼び出しが交互になる」だけでは根拠にならない。

「変わる」に当てはまっても、次の 4 つは Decision にしない。トリレンマが成立していないか、より軽い書き方で同じ保証が得られる。

- 分岐の条件が入力だけで決まる。最初の取得より前に判断が確定するので、取得と判断を交互に進める必要がない
- 分岐の片方が失敗して止まり、もう片方はそのまま続くだけ。続く側で取得するものが判断によって変わらないなら、失敗を早く返しているだけで、取得の並びは 1 本のまま
- 分岐を「取得キーの集合が空になる」に変換できる。候補を返す domain の純粋関数を置けば、判断による分岐は消える。当てはまるのは、呼ぶ取得の操作は変わらず渡すキーの数だけが変わる場合に限る。取得するかどうかが判断で変わり、その取得がキーの集合を取らない形（ある主体に紐づく一覧、集計、存在確認）なら、空のキーに畳めないので Decision にする
- 判断と取得の往復が 1 回で終わり、呼ぶ取得の操作が 1 つに固定されている。判断の結果を domain だけが構築できる取得キーの型にし、取得したエンティティと合わせて解決済みの型を作れば、判断を経ていない値がその先に流れない（ゼロ値は外からも書けるので、止まるのは有効な値だけ）。判断の結果によって呼ぶ取得の操作自体が変わるなら、キーの型では表せないので Decision にする。取得するかどうか自体が変わる場合も「1 つに固定されている」には当たらない。キーの型では「取らない」を表せず、`Option` にすると判断が interpreter 側に戻る

どれにも当てはまらず、判断の結果によって以降の取得の並びが変わるなら Decision を使う。状態を型にして網羅を lint に検出させる価値が、状態と interpreter を書くコストを上回る。

「取得キーの集合が空になる」の除外では、候補が空のときに取得を省くガードが interpreter 側に残る。これは「取得しても結果が空だと分かっている」という性能上のもので、設計原則で domain に出すことにしている「判断を伴う取り出し」には当たらない。省かずに空のキーのまま呼んでも構わない。

## 状態が持つもの

Need 状態が持つのは次の 2 つだけ。終端状態 (Decided / Failed) は判定の結果そのもの (`Mutations` / `Err`) を持つのでこの制限の対象外。

- この状態の取得キー（何を取得するかを示す ID など）
- ここまでに確定した事実

事実は 1 回の取得（または 1 回の判定）が確定させる単位で struct に分け、状態はそれを embed する。フィールドを状態に直接並べない。

```go
// xxxFactsArticle は Article 取得で確定する事実。
type xxxFactsArticle struct {
    ArticleID ArticleID
    AuthorID  AuthorID
}

// 開始状態。まだ何も確定していないので取得キーだけを持つ。
type XxxNeedArticle struct {
    ArticleID ArticleID
}

func (XxxNeedArticle) isXxxDecision() {}

func (s XxxNeedArticle) Decide(article Article) XxxDecision {
    if article.Visibility != VisibilityPublic {
        return XxxFailed{Err: ...}
    }
    return XxxNeedBlock{
        xxxFactsArticle: xxxFactsArticle{ArticleID: article.ID, AuthorID: article.AuthorID},
    }
}

// 取得キーは確定済みの AuthorID なので、状態に重複して持たない。
type XxxNeedBlock struct {
    xxxFactsArticle
}

func (XxxNeedBlock) isXxxDecision() {}

func (s XxxNeedBlock) Decide(block Option[Block]) XxxDecision {
    if block.IsSome() {
        return XxxFailed{Err: ...}
    }
    return XxxNeedThreadAuthorIDs{xxxFactsArticle: s.xxxFactsArticle}
}
```

分ける基準は「いつ確定したか」であって「何に使うか」ではない。用途で分けると、後から読む場所が増えたときに分類が変わる。確定時点は変わらない。

空のフィールドを持ちうる型を作らない。ある値が未確定の段があるなら、その値はその段の型に入れない。取得済みの値をすべて 1 つの struct にまとめると、序盤の状態でゼロ値のフィールドを持つことになり、コンパイラが検出できない穴ができる。

Decide が読まないフィールドを状態に直接置かない。終端まで運ぶだけの値も事実の struct に入れる。状態に直接あるフィールドは取得キーだけになる。取得キーが既に確定した事実の中にあるなら重複して持たず、どれをキーに使うかをコメントで示す。

事実が分岐なく積み上がる区間は、事実の struct を入れ子にする。前段の事実を含めておくと、遷移時のコピーが 1 行で済む。平らに並べると事実の数だけコピーが増え、事実を足したときの書き漏らしも増える。

```go
type xxxFactsParentComment struct { ArticleID ArticleID; ParentAuthorID AuthorID }

// 前段の事実を含める
type xxxFactsArticle struct {
    xxxFactsParentComment
    AuthorID AuthorID
}

type XxxNeedThreadAuthorIDs struct {
    xxxFactsArticle
}

func (s XxxNeedBlock) Decide(block Option[Block]) XxxDecision {
    ...
    return XxxNeedThreadAuthorIDs{xxxFactsArticle: s.xxxFactsArticle}
}
```

入れ子にしても昇格するので、読む側は `s.ArticleID` のままで変わらない。

入れ子は「ここまでの履歴」を型にしたものなので、その状態に別の履歴で到達する経路があると組み立てられない。`xxxFactsArticle` に親コメントの事実を含めた場合、親コメントを経由しない経路からは作れない。この場合は前段の事実を合流点で落とし、鎖をそこから始め直す。

枝分かれ自体は問題にならない。経路ごとに違う事実が増えても、共通の前段を含む鎖が 2 本になるだけで、両方を含む型は要らない。

事実の struct にマーカーメソッドを付けない。付けると sum type のメンバーになり、interpreter の switch に意味のない case が必要になる。

型は非公開にする。フィールドが公開なら昇格して interpreter から取得キーとして読めるので、型を公開する理由はない。

## 分岐と合流

状態は 1 つの事実集合を持ち、そこへ入ってくる全経路がそれを構築できなければならない。判定はこの一致だけで行う。コメント返信の作成可否を例にする（型名の `ReplyCreate` プレフィックスは図では省略）。

```
NeedParentComment → NeedArticle ─ 投稿者が著者本人 ──────→ NeedThreadAuthorIDs → Decided
                                └ 他人 → NeedBlock ─────┘
```

`NeedArticle` は Article を取得して `{ArticleID, AuthorID}` を確定させ、投稿者が著者本人かどうかで分岐する。他人ならブロック関係を確認する `NeedBlock` を挟む。

この合流は本物。`NeedBlock` はブロックされていれば `Failed`、されていなければ次へ進むだけで、取得した `Block` はその場の判定に使って捨てる。事実を増やさないので、`NeedThreadAuthorIDs` に入る事実はどちらの経路でも `{ArticleID, AuthorID}` で一致する。このように、通ってきた経路が余分に取得した値を合流点で落とすのは正当。

ここで「`NeedBlock` を通ったときだけ分かる値」を `NeedThreadAuthorIDs` に持たせたくなったら、その合流は偽物。著者本人の経路では埋められず、ゼロ値か「未取得を表す Option」になる。`NeedThreadAuthorIDs` 以降を 2 系統に分ける。

もう 1 つの本物の合流は、同じ事実を別の経路で確定させる形。

```
入力が ArticleID ──────────────────────────→ NeedArticle
入力が ParentCommentID → NeedParentComment ─┘
```

どちらも `ArticleID` を確定させる。取得元が違うだけで確定する事実は同じなので合流できる。分けると `NeedArticle` 以降のチェック列が丸ごと重複する。

合流させるか状態を分けるかは、その先の長さで決める。末尾が長いほど合流の価値が高い。1〜2 状態なら分けた方が読みやすい。

Option 型を使ってよいのは「確定した結果として値が無い」ときだけ。「まだ取得していない」を `None` で表さない。前者は事実、後者は穴。

事実を struct にまとめておくと、事実集合の不一致は「その struct を渡していない」という 1 箇所の欠落として現れ、struct リテラルの網羅を検査する lint (`exhaustruct` など) が検出できる。フィールドを状態に直接並べていると、埋め忘れがゼロ値に紛れて残る。

## 設計原則

- Decide 引数にはプリミティブ値よりエンティティを渡す（間違ったフィールドを渡すリスクを減らす）
- Decided / Failed も含めすべての状態を struct 型で定義する
- 状態は必ず値で返す。ポインタで返すと `go-check-sumtype` は `Xxx` と `*Xxx` を同じメンバーとして扱うため lint は通るが、`case Xxx:` にマッチせず実行時に `default` へ落ちる
- 状態は interface と同じパッケージに置く。別パッケージで宣言した型は interface を満たしていてもメンバーに数えられず、case の書き忘れが検出されない
- 分岐を持たない Need 状態を作らない。判断がなく事実を書き換えるだけの状態は、1 つの Need にまとめて Decide に複数の引数として渡す形に畳む。畳めるのは取得キーがその状態の時点ですべて確定している場合に限る。畳んだ取得を interpreter 側で並列にしてよいかはデータストアの制約に従う。並行読み取りを想定していないトランザクションの中では直列に取る
- 次の取得キーが直前の取得結果からしか作れない場合も畳んでよい。interpreter が `repo.GetArticle(ctx, parent.ArticleID)` のように ID フィールドを読んで次の取得に渡すのは構わない。判断を伴う取り出し（Option の分岐、条件による選択）は domain の関数に出す

## 命名

`Xxx` は判定の対象を表す名詞句にする (`Publish`, `Reply`, `DeliveryAvailability`)。何を判定しているかが型名だけで分かるようにする。

同じ操作を純粋関数 1 本で書いたものが既にあるなら、その関数名の `Xxx` をそのまま使う (`ResolvePublish` ↔ `NewPublishDecision`)。どちらで書いても、また後から移しても名前が変わらない。

エンティティ名だけ (`ArticleDecision`) にしない。何の判定か読めず、同じエンティティを扱う別の操作と衝突する。判定結果 (`CanReplyDecision`) でも名付けない。終端が増えたときに名前が実態と合わなくなる。

| 対象 | 命名 | 例 |
|---|---|---|
| interface | `XxxDecision` | `PublishDecision` |
| マーカーメソッド | `isXxxDecision()` | `isPublishDecision()` |
| 終了状態 | `XxxDecided` | `PublishDecided` |
| 失敗状態 | `XxxFailed` | `PublishFailed` |
| Need 状態 | `XxxNeedYyy` | `PublishNeedArticle` |
| 事実の struct | `xxxFactsYyy` | `publishFactsArticle` |
| コンストラクタ | `NewXxxDecision` | `NewPublishDecision` |
| 遷移メソッド | `Decide` | `Decide(article Article) PublishDecision` |

`Decide` は状態から次の状態への辺を示す唯一の手がかりなので、遷移メソッドの名前はこれに固定する。別名にするとその状態は遷移を持たない終端として扱われる。状態遷移をたどる静的解析もこの名前を起点にする。

Need 状態の `Yyy` は取得するもの、事実の `Yyy` はそれを確定させた取得を表す。`XxxNeedArticle` が取得した結果が `xxxFactsArticle` になる。

パッケージ内に Decision が 1 つしかなくても `Xxx` を省略しない。2 つ目が来たときに既存の型を全部改名することになる。

終端は `XxxDecided` / `XxxFailed` の 2 つが最小構成。interpreter がその後に行う処理で分かれるなら、結果を表す名前で終端を増やしてよい (`XxxSkipped` / `XxxGenerate` など)。

## 実装手順

1. sum type interface を定義
   - `//sumtype:decl` は必須。これがないと `go-check-sumtype` の網羅チェックが働かない
   - `//decision:decl` も付ける。`//sumtype:decl` は Decision パターンと無関係な sum type にも付くので、Decision の状態機械であることはこちらで宣言する
   ```go
   //sumtype:decl
   //decision:decl
   type XxxDecision interface { isXxxDecision() }
   ```

2. マーカーメソッドは状態ごとに書く
   - 共通の非公開 struct を作って embed してはならない。マーカー struct 自身も interface を満たすためメンバーに数えられるが、非公開だと interpreter 側の switch では case に書けず、網羅チェックが永久に通らなくなる
   - interface そのものを埋め込む形 (`type XxxNeedArticle struct { XxxDecision; ... }`) でもマーカー型が消えるので網羅チェックは効くが、埋め込みフィールド名 `XxxDecision` は公開扱いなので `exhaustruct` の対象にすると全リテラルに `XxxDecision: nil` が要る。採らない
   - 状態の型を `exhaustruct` の対象から外せばその `nil` は省けるが、事実の struct を渡し忘れたときの検出も同時に失う。マーカーの都合でこの検出を捨てない
   - 状態ごとに 1 行書く手間と引き換えに、網羅チェックが実際に効くようにする

3. 終了状態を定義（判定の結果そのものを持つ）
   ```go
   type XxxDecided struct {
       Mutations []Mutation
   }
   func (XxxDecided) isXxxDecision() {}
   ```

4. 失敗状態を定義
   ```go
   type XxxFailed struct {
       Err error
   }
   func (XxxFailed) isXxxDecision() {}
   ```

5. 事実の struct を定義
   - 1 回の取得（または判定）が確定させる値をまとめる
   - 型は非公開、フィールドは公開

6. 各 NeedYyy 状態を定義
   - 内部プロパティ: 取得キーと、そこまでに確定した事実の struct
   - Decide 引数: 取得したエンティティと外部環境情報 (now など)
   ```go
   // 開始状態。まだ何も確定していないので取得キーだけを持つ
   type XxxNeedArticle struct {
       ArticleID ArticleID
   }
   func (XxxNeedArticle) isXxxDecision() {}
   func (s XxxNeedArticle) Decide(article Article, now time.Time) XxxDecision { ... }

   // Article 取得後の状態。確定した事実を embed する
   type XxxNeedYyy struct {
       xxxFactsArticle
       YyyID YyyID
   }
   func (XxxNeedYyy) isXxxDecision() {}
   func (s XxxNeedYyy) Decide(yyy Yyy, now time.Time) XxxDecision { ... }
   ```

7. コンストラクタを定義
   ```go
   func NewXxxDecision(id ArticleID) XxxDecision {
       return XxxNeedArticle{ArticleID: id}
   }
   ```

## interpreter

状態が 1 段で終わる Decision も for ループで書く。1 周で抜けるだけだが、段が増えたときに interpreter の形から書き直さずに済む。

`now` はループの前で 1 回だけ取る。Need ごとに取り直すと、1 つの判定の中で段ごとに基準時刻がずれる。

case が漏れると `decision` が更新されないまま無限ループになる。これを防ぐのは `go-check-sumtype` だけなので、`//sumtype:decl` を必ず付け、lint からこのチェックを外さない。golangci-lint 経由ではパッケージをまたぐ宣言を認識できないため、直接実行する（https://github.com/alecthomas/go-check-sumtype/issues/5）。

`-default-signifies-exhaustive=false` を付ける。付け忘れると `default` があるだけで網羅済みとみなされ、チェックが素通しになる。`default` は lint を通さずに動かしたときの保険として置く。

取得とコミットの書き方はリポジトリごとに違う。形だけを示す。

```go
now := 現在時刻
decision := domain.NewXxxDecision(id)
for {
    switch d := decision.(type) {
    case domain.XxxDecided:
        return 変更を適用する(d.Mutations)
    case domain.XxxFailed:
        return d.Err
    case domain.XxxNeedArticle:
        article, err := Article を d.ArticleID で取得する
        if err != nil { return err }
        decision = d.Decide(article, now)
    case domain.XxxNeedYyy:
        yyy, err := Yyy を d.YyyID で取得する
        if err != nil { return err }
        decision = d.Decide(yyy, now)
    default:
        return fmt.Errorf("unknown decision type: %T", d)
    }
}
```

## 参考資料

- [Domain model purity vs. domain model completeness (DDD Trilemma)](https://enterprisecraftsmanship.com/posts/domain-model-purity-completeness/)
- [The CAP theorem of domain modeling - Vladimir Khorikov](https://vkhorikov.medium.com/the-cap-theorem-of-domain-modeling-2e3763301caf)
- [F# free monad recipe - Mark Seemann](https://blog.ploeh.dk/2017/08/07/f-free-monad-recipe/)
