# go-decision-pattern

Decision パターンの規約と、それを検証するツール。

規約の本文: [rule/decision-pattern.md](https://github.com/tomoemon/go-decision-pattern/blob/main/rule/decision-pattern.md)

## Decision パターンとは

判定を「次に何が必要か」の連なりとして型で書き、実際の取得は呼び出し側に任せる。domain は純粋関数のまま、取得は必要な分だけ逐次行える。

```go
// domain: 判断だけ。DB を触らない
func (s XxxNeedArticle) Decide(article Article) XxxDecision {
    if article.Visibility != VisibilityPublic {
        return XxxFailed{Err: ErrNotPublic}
    }
    return XxxNeedBlock{xxxFactsArticle: xxxFactsArticle{AuthorID: article.AuthorID}}
}

// 呼び出し側: 状態が要求したものを取ってきて渡すだけ
for {
    switch d := decision.(type) {
    case XxxNeedArticle:
        article, err := repo.GetArticle(ctx, d.ArticleID)
        if err != nil { return err }
        decision = d.Decide(article)
    case XxxNeedBlock:
        ...
    }
}
```

解こうとしているのは [DDD トリレンマ](https://enterprisecraftsmanship.com/posts/domain-model-purity-completeness/)。domain がプロセス外の依存に触れないこと（Purity）、ドメインロジックが分断されず domain にすべて収まっていること（Completeness）、余計な取得をしないこと（Performance）は、素直に書くとどれか 1 つを諦めることになる。

| 素直な書き方 | 諦めるもの |
|---|---|
| domain が repository を呼ぶ | Purity。domain が DB に依存し、テストに DB が要る |
| 呼び出し側で先に全部取って domain に渡す | Performance。使わないデータまで毎回取る |
| 判断を呼び出し側に散らす | Completeness。ロジックが domain の外に漏れる |

Decision パターンは「次に何が必要か」を状態として返すことで 3 つとも満たす。

## 何が得られるか

- domain に DB アクセスが入らない。テストは値を渡すだけで書ける
- 判断が呼び出し側に漏れない。`if` はすべて `Decide` の中にある
- 取得は判断の結果に応じた分だけになる。早く終わる経路では後続を引かない
- 状態の網羅を lint が保証する。分岐を足して `case` を書き忘れると実行前に落ちる
- 取りうる経路を静的に復元できる。[flow](https://github.com/tomoemon/go-decision-pattern/blob/main/internal/flow/README.md) が状態遷移からフローチャートを生成する

## 何を払うか

- 型が増える。1 つの判定に interface が 1 つ、状態が段数分、事実の struct がそれと同程度。5 段の判定なら型は 10 を超える（この冗長さがどこから来ているかは [docs/encoding.md](https://github.com/tomoemon/go-decision-pattern/blob/main/docs/encoding.md)）
- 呼び出し側に for-switch のボイラープレートが要る。段の数だけ `case` が並ぶ
- `go-check-sumtype` が実質必須になる。`case` が漏れると状態が更新されないまま無限ループする。golangci-lint 経由ではパッケージをまたぐ `//sumtype:decl` を認識できないため、lint とは別に直接実行する必要がある
- 「どこで何が確定したか」を設計する手間がかかる。事実をまとめる単位を誤ると、ゼロ値を持つ状態ができて型の保証が崩れる
- 全体のフローがコード上のどこにも書かれない。domain は状態ごとに分かれ、呼び出し側は順序を持たないため。[flow](https://github.com/tomoemon/go-decision-pattern/blob/main/internal/flow/README.md) でフローチャートを生成し、どの順に何を取り、どの条件でどこへ分岐するかを目で追えるようにする

## 使いどころ

判定条件は「domain の判断結果によって、次に取得するものが変わるか」。変わらないなら必要ない。取得が何段階に分かれていても、取得するものが判断に依存しないなら、呼び出し側で全部取って純粋関数に渡せば済む。

「変わる」に当てはまっても Decision にしない場合が 4 つある。詳細は規約の [適用基準](https://github.com/tomoemon/go-decision-pattern/blob/main/rule/decision-pattern.md#適用基準) を参照。

## lazy loading との違い

同じトリレンマの文脈で lazy loading が引き合いに出されるが、別物になる。lazy loading は実行時に domain が I/O を起こし（ORM がそれを隠す）、対象は ORM がマップした関連の辿りに限られ、何本クエリが飛ぶかは実行するまで分からない。そもそも Go には成立の前提となる言語機能が無い。

検討の詳細は [docs/lazy-loading.md](https://github.com/tomoemon/go-decision-pattern/blob/main/docs/lazy-loading.md)。


## 規約を取り込む

規約は Markdown が 1 つあるだけなので、コピーして AI エージェントが読む場所に置けばよい。Go の依存は要らない。

```sh
curl -Lo .claude/rules/decision-pattern.md \
  https://github.com/tomoemon/go-decision-pattern/releases/latest/download/decision-pattern.md
```

本文は各リリースに添付してあるので、この URL は版が上がっても変わらない。特定の版が要るなら `latest` をタグに置き換える。

置き場所はエージェントに合わせる（Claude Code なら `.claude/rules/`、Codex なら `AGENTS.md` から参照するなど）。

本文の先頭に規約バージョンの行がある。手元の写しがどの版かはそこで分かるので、[releases](https://github.com/tomoemon/go-decision-pattern/releases) と見比べて、上がっていれば取り込み直す。写しを勝手に書き換えていないかは差分で確かめられる。

```sh
ver=$(sed -n 's/^規約バージョン: \(v[0-9.]*\).*/\1/p' .claude/rules/decision-pattern.md)
diff <(curl -sL https://github.com/tomoemon/go-decision-pattern/releases/download/$ver/decision-pattern.md) \
     .claude/rules/decision-pattern.md
```

対象を絞るなら frontmatter を足す。無いとリポジトリ全体向けの指示として扱われ、Decision と無関係な作業でも読み込まれる。

```yaml
---
paths:
  - "domain/**/*.go"
  - "application/**/*.go"
---
```

複数のリポジトリで共有する場合、片方だけ直すと文面が分岐する。本文はこのリポジトリに 1 つだけ置き、取り込む側はコピーを commit する。リポジトリ固有の事情（層の呼び名、lint の設定、そのリポジトリでの実例）は別ファイルに分けて置く。

## ツールを使う

フローチャートの生成や、規約に反する形の検出をしたい場合だけ入れる。

```sh
go get -tool github.com/tomoemon/go-decision-pattern/cmd/decision-pattern
```

入れると規約の書き出しもコマンドでできる。frontmatter の注入と、生成物である旨の注記が付く。版が `go.mod` で固定されるので「どの版の規約に従っているか」がコミットに残る。

```sh
go tool decision-pattern rule \
  -path "domain/**/*.go" \
  -path "application/**/*.go" \
  -o .claude/rules/decision-pattern.md
```

更新は `go get -u` して書き出し直す。CI で次を回せば、書き出し忘れを検出できる。

```sh
go tool decision-pattern rule -path ... -o .claude/rules/decision-pattern.md
git diff --exit-code .claude/rules/decision-pattern.md
```


### フローチャートを生成する

`//decision:decl` が付いた sum type の状態遷移を解析し、取りうる経路を Mermaid で出す。到達しない状態や `nil` の返却など、規約に反する形も報告する。

```sh
go tool decision-pattern flow ./domain/...              # 標準出力
go tool decision-pattern flow -o docs/flow ./domain/... # Decision ごとに 1 ファイル
```

図の読み方、警告の種類、解析の前提と制約は [internal/flow/README.md](https://github.com/tomoemon/go-decision-pattern/blob/main/internal/flow/README.md) を参照。
