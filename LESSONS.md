# LESSONS

goup 開発で得た教訓を蓄積する。同じ落とし穴を二度踏まないためのチェックリスト。

## Fast-fail 権限チェック / sudo PATH バグ修正 (2026-07-03)

### sudo は `secure_path` で `$PATH` を剥奪する
- Ubuntu の `/etc/sudoers` 既定は `Defaults secure_path=/usr/sbin:/usr/bin:/sbin:/bin`。`sudo` 実行時にユーザー PATH は完全に置換され、`/usr/local/go/bin` などが消える。
- 結果として `exec.Command("go", ...)` は `sudo` 下では `executable file not found in $PATH` で死ぬ。
- **ルール**: sudo で走る可能性のあるバイナリからシステムツールを呼ぶ場合、必ず絶対パスで `exec.Command(filepath.Join(installRoot, "go", "bin", "go"), ...)` するか、ファイル直読み等でサブプロセス起動そのものを避ける。

### `go env GOVERSION` / `go version` は go.mod の `toolchain` directive で汚染される
- カレントディレクトリの `go.mod` に `toolchain go1.26.4` があると、`go version` は `/usr/local/go` の実体（例: 1.26.3）ではなく auto-download された 1.26.4 を報告する。
- 「実際に install されている Go のバージョン」を知りたい局面（updater/rollback ツール等）ではこれは致命的な誤解を生む。
- **ルール**: install root の Go バージョンを知る目的では `<installRoot>/go/VERSION` の1行目を直読みする。`go` バイナリを叩かない。副次効果として rollback 直前の壊れた go でも動く。

### 副作用のある操作は「無料の読み取りチェック」を前に置く
- `goup update` は sudo なしだと `~70MB` DL + sha256 検証を完了してから `Backup` の `os.Rename` で初めて permission error になっていた。無駄で遅い。
- **ルール**: destructive な後段処理の前に、副作用ゼロで失敗条件を検知できる手段があるなら必ず前段に置く。特にネットワーク I/O やディスク I/O の前に権限/前提チェックを済ませる。

### 権限プローブは probe ファイル方式が最も堅牢（stdlib only）
- `unix.Access(dir, W_OK)` は `golang.org/x/sys/unix` 依存で stdlib-only 方針に反する。
- `os.Stat` + uid 判定は ACL / read-only mount / root-squash NFS 等の実効権限を見落とす。
- `os.CreateTemp(dir, ".probe-*")` → 即 `os.Remove` は、カーネルに実効書き込み可否を問い合わせるため上記全てを正しく判定できる。
- **ルール**: stdlib のみで書き込み権限を判定したい場合、probe ファイル方式を採用する。

### 実機テストは単体テストで届かない領域を暴く
- 今回の `sudo goup update` 起動時クラッシュ（PATH バグ）は、`httptest.Server` + `t.TempDir()` の単体テストでは絶対に再現できない。sudo 環境そのものが再現不能。
- 「テストが green だから完成」ではない。本番相当の実行環境（sudo, system directory, 実際のダウンロード先）で通す一手間を必ず入れる。
- **ルール**: `/usr/local` や sudo が絡むツールは、実装完了 → PR 前に必ずダウングレード → update → rollback の一連を実機で通す。CI では網羅できない権限/PATH 系バグはここで炙り出す。

### 成功時の沈黙は UX 悪、対称性を保つ
- `Update` は `Updated: X -> Y` と出るが `Rollback` は何も出さず終了していた。ユーザーは「本当に動いた？」と不安になる。
- **ルール**: ユーザーが起動した副作用のあるコマンドは、成功時にも 1 行の完了サマリーを出す。姉妹コマンド間で出力対称性を保つ（`Updated: ...` ⇔ `Rolled back to ...`）。

### エラーメッセージの hint はサブコマンド名を hard-code しない
- `wrapPermissionError` の hint が `rerun with sudo, e.g. `sudo goup update`` に決め打ちで、`goup rollback` の失敗時にも同じ hint が出て不正確だった。
- **ルール**: 共通エラーラッパーは呼び出し側のサブコマンド文脈を知らないので、hint はサブコマンド名を含めず汎用形（`rerun with sudo`）にする。特定のコマンド例が本当に有益な場面では呼び出し側で文脈込みで組み立てる。

## v0.2.0: install / list / version 実装 (2026-07-03)

### Go 標準 `flag` パッケージは flag と positional を interleave できない
- `install 1.27rc1 --pre`（自然な CLI 順序）で `--pre` が positional 扱いされ `NArg()==2` で失敗した。`flag.Parse` は最初の非フラグトークンで停止する仕様（`go doc flag` に明記）。
- **ルール**: 「flag と 1 個の positional を任意順で受ける」サブコマンドは、以下の loop-parse パターンで書く。将来のサブコマンド追加時にコピペで使える定石として覚えておく。
  ```go
  var version string
  for len(args) > 0 {
      if err := fs.Parse(args); err != nil { return err }
      if fs.NArg() == 0 { break }
      if version != "" { return fmt.Errorf("only one positional allowed") }
      version = fs.Arg(0)
      args = fs.Args()[1:]
  }
  ```
- 併せて `flag.ContinueOnError` + `fs.SetOutput(io.Discard)` を使う。`ExitOnError` だと loop-parse が回せない上、`main()` 側の "goup: %v" プレフィックス出力と二重印字になる。

### ユーザーからの識別子入力は必ず case-normalize する
- `NormalizeVersion("Go1.26.3")` が `"goGo1.26.3"` を返して release list とマッチしなかった。go.dev API は全て lowercase なのに、ユーザー入力の大文字混在を想定していなかった。
- **ルール**: 外部 API との文字列マッチに使うユーザー入力は、`strings.ToLower` + `strings.TrimSpace` を必ず前段で通す。表記揺れは「拾えるところで全部拾う」のが CLI 設計の基本姿勢。

### UI 装飾（区切り線/省略記号）は「後続コンテンツが確定してから」出力する
- `goup list` の window 外 fallback で、`  ...` を先に印字してから releases を検索し、見つからないと dangling `...` だけ残るバグがあった。dev build や API から drop された古いバージョンで踏む。
- **ルール**: 装飾行（`...` / 罫線 / セパレータ）は、その直後に必ず content 行が続くことを確認した後で emit する。「印字→検索」ではなく「検索→（見つかれば）印字＋content」の順で書く。

### テスト fake データはプロダクションデータの命名規則に従わせる
- Install テストで `"goSTABLE"` / `"goPRErc1"` の mixed-case fake version を使っていたら、後で追加した `NormalizeVersion` の lowercase 化で軒並みマッチしなくなった。fake は「架空だが production data のフォーマット規約を守る」姿勢が必要。
- **ルール**: fake データを作る際は「本物と区別しやすい語彙」（`goOLD` / `goSTABLE`）を選びつつ、命名規則（この場合は全 lowercase）は本物と揃える。「区別のためわざと違う形にする」誘惑に負けない。
