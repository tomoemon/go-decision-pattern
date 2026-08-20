# lazy loading との違い

DDD トリレンマの文脈で lazy loading が引き合いに出されることが多い。Decision パターンがそれと同じものなのか、劣るのかを検討した記録。

参照した記事は 2 つ。

- [Domain model purity vs. domain model completeness](https://enterprisecraftsmanship.com/posts/domain-model-purity-completeness/)
- [Domain model purity and lazy loading](https://enterprisecraftsmanship.com/posts/domain-model-purity-lazy-loading/)

## 記事が扱っている lazy loading

具体例は `User` 集約と `LoginSession` のコレクション。

```csharp
public class User
{
    private List<LoginSession> _loginSessions;

    public virtual void RegisterSession(DateTime now)
    {
        LoginSession session = _loginSessions.Last();   // ここで DB が読まれる
        ...
    }
}
```

遅延させているのは ORM のランタイムプロキシ。EF Core が実行時に `User` を継承したクラスを生成し、`virtual` なプロパティの getter を上書きして、最初のアクセスで SELECT を投げる。`User` 自身は EF を参照しない。

著者が挙げる Purity の判定基準は 3 つ。

1. domain クラスがプロセス外の依存やアプリケーション層のクラスを明示的に参照しない
2. domain への入力がすべて参照透過（domain クラス自身への呼び出しを除く）
3. 副作用が domain 内に閉じている

`_loginSessions.Last()` が DB を叩くのは 2 番の例外条項に当たるので Purity を壊さない、という論理になっている。一方 `ILazyLoader` を注入する明示的な lazy loading は「The `User` now depends on `ILazyLoader`, which is not a POCO class」として 1 番と 2 番の違反としている。

## 隠された依存注入である

判定基準が見ているのは「実際に I/O が起きないこと」ではなく「ソース上に依存が見えないこと」。実行時には domain のメソッドの途中で DB が読まれる。依存が消えたのではなく、ORM が注入を代行して見えなくしている。

著者自身のトリレンマの分類でも「domain クラスに外部依存を注入する」は Purity を諦める解法として挙がっており、プロキシによる lazy loading はそれを隠蔽したものと見ることもできる。

Go で repository の interface を注入した場合と比べると、実行時に起きることは同じ。domain のメソッドの途中で I/O が発生し、呼ぶたびに結果が変わりうる。それでも差は残る。

| | C# プロキシ | Go で repo を注入 |
|---|---|---|
| ソース上の依存 | 無い | 有る |
| domain が呼べる範囲 | マップされた関連のみ | interface の全メソッド |
| テスト用インスタンスの依存 | 無い | 有る |

3 行目が実務上いちばん効く。C# のプロキシは EF がインスタンスを作るときにだけ生成されるので、テストで `new User(...)` と書いたインスタンスはただの POCO で、遅延読み込みの挙動を持たない。読み込みが関係しないテストではダミーが要らない。Go の注入では依存が型そのものに入っているので、常にダミーを渡すことになる。

## Decision パターンとの違い

| | lazy loading | Decision |
|---|---|---|
| 実行時に domain が I/O を起こす | する | しない |
| ソース上の依存 | 無い（ORM が隠す） | 無い |
| 対象 | ORM がマップした関連の辿りのみ | 任意の問い合わせ（存在確認・集計・別集約） |
| 取得の計画 | 実行するまで分からない | 型に書かれ、静的に列挙できる |
| コスト | ほぼゼロ（ORM 任せ） | 型とボイラープレートが増える |

解こうとしている問題も違う。記事の主眼は「集約を大きく保ったまま Performance の劣化を抑える」ことで、集約設計の道具として位置づけられている。Decision が扱うのは「どの問い合わせを、どの順で、どの条件で出すか」という取得の並びで、集約の大きさとは別の軸になる。

## Go では選べない

lazy loading が成立するのは、次の 2 つが揃う言語に限られる。

- プロパティアクセスがメソッド呼び出しであること
- `virtual` により、実行時に生成した派生クラスでそれを差し替えられること

Go にはどちらも無い。構造体のフィールドアクセスは単なるメモリ参照で、割り込む余地がない。そのため関連を遅延で辿ろうとすると、エンティティが DB クライアントを持つ形（`ent` の `user.QueryOrders(ctx)` など）になり、著者が Purity 違反とした明示的な lazy loading と同じ構造になる。

## 結論

同じものではなく、劣ってもいない。実行時に domain が I/O を起こすかどうかで根本的に分かれ、扱える問い合わせの範囲も違う。Go では lazy loading がそもそも選択肢に入らない。

Decision パターンの位置づけは、著者の 3 解法のどれでもない。3 番目（判断を層で分割）は Completeness を諦めるが、Decision は判断そのものを domain のデータとして返すことで Completeness を保ったまま Purity と Performance を取る。トリレンマの 3 軸では説明できない対価、つまりコード量と型の数を第 4 の通貨として支払っている。

## 出典の扱いについて

この文書のうち、記事の主張は「記事が扱っている lazy loading」の節まで。「隠された依存注入である」以降はこのリポジトリ側の整理で、著者が述べているものではない。特に次の 3 点は記事に書かれていない。

- lazy loading の対象が ORM のマップした関連に限られること。記事は集約の関連の辿りしか扱っておらず、限界として述べているわけではない。プロキシが横取りできるのはマップされたメンバーだけだという性質からの推論
- Go で成立しないこと。記事は C# と EF Core が前提
- N+1 のリスク。一般に知られた副作用として補ったもの
