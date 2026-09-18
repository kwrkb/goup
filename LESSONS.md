# LESSONS

goup 開発で得た教訓を蓄積する。同じ落とし穴を二度踏まないためのチェックリスト。

## v0.3.0 リリース後: --help 再設計 & PR レビュー対応 (2026-07-04)

### 標準 `flag` パッケージの `Usage of <name>:` は UX の罠

- `flag.NewFlagSet("check", flag.ExitOnError).Parse(os.Args[2:])` と書くだけだと、`goup check --help` は `Usage of check:` の 1 行だけ返す（フラグ定義が無いため body が空）。「フラグが無いから help は自動で薄くて OK」は誤り—**ユーザーは "何をするコマンドか" を最初にここに探しに来る**。空の Usage は「詳細を教えないコマンド」に見える。
- `ContinueOnError` + loop-parse を組み合わせた `install` では、`--help` が `flag.ErrHelp` として parseInstallArgs から返り、`main()` で `goup: flag: help requested` の**エラー扱い**で exit 1。help を求めたユーザーがエラーメッセージを受け取る最悪の UX。
- **ルール**: 各サブコマンドの help を flag パッケージ任せにしない。help テキストは flag 定義とは別のソース（定数マップ）に置き、dispatch 前に `--help`/`-h`/`-help` を intercept する。flag.ErrHelp を各 parser で握るのは対症療法（parser が増える度に必要）で、dispatch 層で 1 回書く方が長期的に安い。

### help の 7 原則は「エージェント最適化と人間 UX が両立する稀なケース」

- 「自己定義 1 行」「read-only vs write のカテゴリ分け」「シグネチャ → 説明 → Arguments → Options → Example の固定構造」「曖昧引数への `(e.g., ...)` 」— これらを守ると、Claude が help だけを読んで正しくコマンドを組み立てられる（`goup help install` → `goup install 1.25.11` 即答）。同時に人間もスキャン速度が上がる。トレードオフではない。
- 特に効くのは **原則 5（階層化）**: トップ help は概要のみ、詳細は `goup help <cmd>` に押し出す。エージェントのコンテキスト節約に直結し、人間の cognitive load も下がる。
- **ルール**: 新規サブコマンドを追加するときは help を後付けの雑務にせず、固定構造テンプレートに沿って書く。README を書く前に help を書く。

### CLI-layer elevation policy は「高頻度 no-op」に例外を切る

- v0.3.0 初版は「書き込み系コマンドは無条件に CLI 層で maybeElevate → 昇格 → 本体呼び出し」の統一設計にした。テスト容易性・実装単純化のメリット重視で、advisor もこれを支持した。
- しかし PR #3 レビューで codex が指摘: `goup update` の「もう最新版」ケースは**高頻度**（毎日 update する CI/cron 運用など）で発生し、そこで毎回 sudo プロンプトが出る/`--no-sudo` で fast-fail するのは v0.2.0 からの明確な UX 回帰。install/rollback の同種 no-op ケースとは頻度が桁違い。
- 対処: `runUpdate` の頭にだけ `isAlreadyLatest(installRoot, baseURL)` の read-only pre-flight peek を追加。read エラーは fall-through で本体に丸投げ。install/rollback は据え置き（低頻度）。
- **ルール**: 「実装対称性」と「実運用頻度」が衝突したら、頻度優先で例外を認める。低頻度の同種ケースまで揃えたくなる誘惑があるが、YAGNI で先送りする方が総合的に安い。ただし判断は必ず `LESSONS.md` の設計判断ログに書き残す—「対称でない」ことが後から見て事故に見えないように。

### AI レビュー bot の指摘は無批判に採用せず、コードで実証する

- gemini-code-assist が `elevate.go` に「`syscall.Exec` は Windows で未定義なので compile fail する」HIGH priority コメントを付けた。もっともらしく、CLAUDE.md の「Windows non-support」方針とも噛み合う。ここで build tag を追加する誘惑がある。
- 実際に `GOOS=windows GOARCH=amd64 go build ./...` を実行 → exit 0 で PE32+ バイナリが生成される。Go の Windows stdlib は `syscall.Exec` シンボルを（runtime error を返す stub として）export しており、compile-time では解決できてしまう。bot の前提が誤り。
- 同時に、bot は最初のコミットしか読んでおらず、後続コミットで解決済みの問題（`flag.ErrHelp` の `--help` エラー化）を 3 件重複指摘してきた。時間軸を bot は理解しない。
- **ルール**: AI レビュー bot の指摘は「一次調査の情報源」であって「採用すべき指示」ではない。特に (a) 「〜は動かない」系の断定は必ず `go build` / 実行して empirical に検証し、(b) 「レビュー対象のコミット SHA」を確認して自分の直近のコミットで既に解決済みでないか照合する。scoping error（古いコミットへの指摘）は非常に多い。

### PR コメント対応は "何を採用しなかったか" とその根拠を明記する

- resolve-pr-comments で 3 種の指摘（gemini Windows / gemini ErrHelp x3 / codex update no-op）のうち 1 件だけ採用、2 件スキップした。コミットメッセージの本文で **スキップした指摘とその理由**（empirical に false / 後続コミットで既に解決）を明示的に列挙した。
- **ルール**: PR コメント対応コミットで「対応した指摘の一覧」と同じ濃度で「スキップした指摘の一覧＋根拠」を書く。後からログを読むレビュアー・自分・別 AI が「なぜ 3 件中 1 件しか触ってないのか」を追跡できる形にする。fold や無視ではなく explicit dismissal。

## v0.3.0: 対話 TTY 自動 sudo 昇格 (2026-07-04)

### TTY 判定を「stdin の ModeCharDevice」だけで済ませると `/dev/null` に負ける
- PLAN 当初案は `os.Stdin.Stat().Mode()&os.ModeCharDevice != 0` で「対話環境か」を判定していた。実装後の smoke test で `goup update < /dev/null` が sudo 昇格側に流れ、`sudo: A terminal is required to authenticate` を吐いた。
- 原因: **`/dev/null` 自体が character device**。stdin redirect の判定に stdin の Mode を見るだけでは根本的に足りない。/dev/zero / /dev/random 等も同じ罠。
- **ルール**: sudo prompt が「本当に出せるか」を知りたいなら、stdin ではなく `os.OpenFile("/dev/tty", O_RDONLY, 0)` の成否で判定する。sudo 自身も `/dev/tty` から password を読むので意味論が 1 対 1 で一致する。

### CI と「stdin redirect」を両方 fast-fail にしたいならハイブリッド判定が必要
- `/dev/tty` open 単独だと、対話シェルからパイプ（`| goup`）や regular-file redirect（`goup < file`）を叩いても controlling terminal は残っているので昇格側に流れる。CLAUDE.md の設計原則「CI / cron / パイプ / redirect は fast-fail」と乖離する。
- 採用: **(a) `/dev/tty` open 可能 AND (b) stdin が character device** の両方成立でのみ対話扱い。(a) が CI/cron/detached を、(b) が pipe/regular-file redirect を担当。それぞれ別の失敗モードを別のプローブで検出する構造。
- 既知の穴として `< /dev/null` だけは (b) をすり抜けるが、stdlib-only 制約下では isatty ioctl 相当（`golang.org/x/term.IsTerminal`）を書かないと閉じられない。トレードオフとして受容し、README / CLAUDE.md に明記する。
- **ルール**: 「非対話」の中に複数の質的に異なる状況（controlling terminal 無し / stdio 系だけ非対話）が混ざる場合、それぞれ独立プローブで AND 判定する。1 個の指標で全部見ようとすると必ずどこかで穴が開く。

### 「非対話の決定的な switch」は環境検知ではなくフラグにする
- どんな精緻な TTY ヒューリスティックも edge case を残す（上記の `< /dev/null`）。スクリプト・CI で確実に非対話動作を保証したいユーザーには、環境検知に頼らせず `--no-sudo` を渡させる。
- **ルール**: 自動判定 + 明示 opt-out フラグの 2 段構え。README では明示フラグをリードで案内する（"For scripts, always pass --no-sudo"）。ユーザーに「fast-fail されなかった場合にも打つ手」を渡す。

### プロセス置換は `syscall.Exec` の一択（`exec.Command` は罠）
- sudo で自己再実行する際、`exec.Command("sudo", ...).Run()` を選ぶと goup が親プロセスとして残り、signal 転送・exit code 中継・stdio 中継を全部書く必要が出る。特に Ctrl-C が sudo に届かない・exit code が変わる等、正しく書くと 30 行くらい増える。
- `syscall.Exec(sudoPath, argv, os.Environ())` はプロセス置換（execve(2)）でカーネル任せなので、そのあたりを全部 sudo に委譲できる。
- **ルール**: 「自プロセスを別コマンドに置き換えたい」が要件なら `syscall.Exec` を選ぶ。「サブプロセスとして走らせて出力を捕まえたい」が要件なら `exec.Command`。この選択は要件で機械的に決まる。

### sudo secure_path を回避するには `os.Executable()` で絶対パスを渡す
- `syscall.Exec(sudoPath, []string{"sudo", "goup", ...}, ...)` だと sudo は自分の secure_path (`/usr/sbin:/usr/bin:/sbin:/bin`) で `goup` を再解決する。`~/go/bin/goup` は消滅し `sudo: goup: command not found`。
- `syscall.Exec(sudoPath, []string{"sudo", "/abs/path/to/goup", ...}, ...)` だと sudo は PATH lookup をスキップして直接 exec する。secure_path 非依存になる。
- **ルール**: sudo 経由で自己再実行するときは `os.Executable()` を必ず argv に載せる。名前だけ渡すのは v0.2.0 の LESSONS で書いた PATH バグの再発。

### 4 変数の分岐は純関数に切り出して table-driven で網羅する
- 「uid・書き込み可否・TTY 有無・--no-sudo フラグ」の 4 変数から「run / elevate / fail」の 3 分岐を決める。判定と副作用（`syscall.Exec` / `checkWritable` / `os.Stdin.Stat`）が同居した関数だと unit test で網羅できない。
- `elevationDecision(uid, canWrite, tty, noSudo) decision` を pure function として切り出し、副作用のある `maybeElevate` は判定結果を dispatch するだけにした。11 パターン table-driven で 3 分岐を網羅できる。
- **ルール**: 副作用のある「起動時判定」ロジックは、副作用を持たない pure な判定関数 + それを dispatch する薄いラッパーに分ける。副作用側は環境依存で unit test 不能でも、判定側は完全網羅できる。

### 「plan は変わる」— empirical evidence が仕様書に勝つ
- PLAN.md は当初 stdin ModeCharDevice で TTY 判定するとしていた。実装 → smoke test で誤動作を確認 → advisor 相談で反論を受けつつも empirical evidence 優先で `/dev/tty` 方式に切り替え → 再度 CLAUDE.md 設計原則との整合を advisor に指摘されてハイブリッド方式に着地。3 段階の pivot。
- **ルール**: PLAN / 設計文書は「作業前の仮説」であって「実装で守るべき契約」ではない。実装中に empirical evidence（実行結果）が仮説を否定したら、迷わず pivot し、PLAN と依存する doc（README・CLAUDE.md・LESSONS.md）を後追いで揃える。「PLAN に書いてあるから」を根拠に妥協した実装を残さない。

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

---

## 設計判断ログ（旧 implementation-notes.md より統合 / 2026-07-26）

実装中に下した判断と、その理由（採用しなかった代替案を含む）。以降の節は旧 `implementation-notes.md` の内容をそのまま移設したもの。

### バグ修正: 権限エラー時に sudo ヒントが出ていなかった

ユーザーから「sudoなしでどうなるか確認」と言われて再現しようとしたところ、`wrapPermissionError` が一度も発火していないことが判明した。原因: `os.IsPermission` は `*PathError`/`*LinkError`/`*SyscallError` を直接受け取った場合しか中身を見ない（`not all errors implementing Unwrap()` — Go標準ライブラリのコメントどおり）実装になっているが、各呼び出し箇所は `wrapPermissionError(fmt.Errorf("...: %w", err))` のように、先に `fmt.Errorf` でラップしてから渡していたため、`os.IsPermission` からは常に非該当の型に見えて `false` を返し続けていた。結果として「sudo で再実行してください」というヒントは実装上決して表示されない状態だった。

修正: `os.IsPermission(err)` を `errors.Is(err, os.ErrPermission)` に置き換えた。`errors.Is` は `Unwrap()` チェーンを再帰的に辿るため、`fmt.Errorf("%w", ...)` で何重にラップされていても正しく検出できる。`t.Chmod(root, 0o555)` で書き込み不可のディレクトリを作り実際に `Backup()` を root権限なしで実行して再現・修正確認し、回帰防止として `TestWrapPermissionError_SeesWrappedError` を `installer_test.go` に追加した。

このバグは既存の `installer_test.go` では検出できていなかった（`wrapPermissionError` 自体を直接テストしていなかったため）。今後、`fmt.Errorf` でラップしたエラーに対してエラー種別判定を行う際は `errors.Is`/`errors.As` を使い、`os.IsPermission` 等のレガシーな型アサーションベースの判定関数をラップ後のエラーに対して使わないよう注意する。

### テスト容易性のための installRoot/baseURL 引数化

`installer.go` の各関数は `installRoot`（デフォルト `/usr/local`）と `Update`/`FetchReleases` の `baseURL`（デフォルト `https://go.dev/dl`）を引数として受け取る設計にした。理由: 実際の `/usr/local/go` を書き換えずに、sha256検証失敗時に展開へ進まないこと・起動確認失敗時に自動ロールバックすること・バックアップが最新1世代のみ残ることを `go test` で自動証明する唯一の方法だったため。`installer_test.go` は `httptest.Server` でダミーの go.dev/dl JSON とダミー tar.gz を返し、`t.TempDir()` を install root として渡して検証する。

### CurrentVersion() に GOTOOLCHAIN=local を付与

ユーザーからのフォローアップ（go.mod の toolchain directive との連携について）を受け、`exec.Command("go", "env", "GOVERSION")` 実行時に `GOTOOLCHAIN=local` を環境変数へ追加した。理由: Go 1.21+ では実行時カレントディレクトリの `go.mod` に `toolchain` 指定があると `go` コマンドが別バージョンを自動解決して報告することがあり、`/usr/local/go` の実体とズレる可能性があったため。`go.mod` 側の toolchain directive 自体の書き換えはスコープ外（README に明記）。

### Backup の世代管理を「新規バックアップ作成前に旧バックアップを削除」で実装

元仕様の「世代管理は最新1世代のみでよい」を、`Backup()` の先頭で既存の `go.bak.*` を削除してから新しいバックアップへ rename する形で実装した。ロールバック可能な状態は常に「直前の1世代」のみで、複数世代の同時保持はしない。

### サブコマンド dispatch は flag.NewFlagSet を各コマンドで生成

現時点でどのサブコマンドもオプションフラグを持たないが、CLAUDE.md の「サブコマンドの dispatch は標準 flag パッケージで実装する」という規約を明示的に満たすため、`os.Args[1]` で分岐した後に `flag.NewFlagSet(name, flag.ExitOnError).Parse(os.Args[2:])` を挟んだ。将来フラグを追加する際の自然な拡張点にもなる。

### tar 展開時の zip-slip 対策

`archive/tar` の展開で、各エントリの結合後パスが `installRoot` 配下であることを確認し、外れる場合はエラーにして展開を中断する（`installer_test.go` の `TestExtract_RejectsPathTraversal` で検証）。go.dev の公式 tarball は信頼できるソースだが、sha256 検証をすり抜けた場合や将来の入力元変更に備えた防御的実装。

### goup update / goup rollback の実機（実際の /usr/local/go 入れ替え）テストは未実施

`sudo` 権限が必要かつ開発環境の Go インストールに直接影響するため、ユーザーの明示的な許可なしには実行しなかった。`goup check` のみ実機（このWSL2環境）で動作確認済み（go1.26.4 = 最新安定版のため "Up to date." を正しく表示）。`update`/`rollback` の中核ロジックは `installer_test.go` の httptest + t.TempDir によるテストで検証している。

### Windows 非対応ガードの検証範囲

`runtime.GOOS == "windows"` の分岐は `GOOS=windows GOARCH=amd64 go build` でのクロスコンパイルが通ることを確認したのみで、実際に Windows / Wine 上で実行してのクラッシュしないことの確認はしていない（このLinux環境では実行できないため）。ロジック自体は単純な文字列比較と `os.Exit(1)` のみで、クラッシュする要素はないと判断した。

### Advisor 指摘を受けた追加修正

一通り実装・テストが揃った時点で Advisor に相談したところ、以下の指摘を受け、いずれも反映した。

1. **`Extract` が実際の go.dev tarball で検証されていない**: `installer_test.go` は合成した1ファイルのみの tar.gz しか使っておらず、実際の配布物に含まれるかもしれない symlink / hardlink / pax header を `switch` の `default` で黙って無視している可能性があった。`installer_manual_test.go`（ビルドタグ `manual` で通常の `go test`/CI からは除外）を追加し、実際に go.dev から現行の linux/amd64 tarball をダウンロードして `Extract` に通し、展開結果の `go version` と `go build` が動くことを確認した（`go test -tags manual -run TestRealArchiveExtracts -v ./...`）。結果: 現行の公式 tarball は symlink 等を含まず、既存の実装で問題なく展開・起動・ビルドできることを実機で確認済み。
2. **ダウンロードにタイムアウトが無かった**: `http.Get` を直接使っていたため、go.dev への接続がハングすると無期限に待ち続ける可能性があった。`installer.go` に `httpClient = &http.Client{Timeout: 5 * time.Minute}` を追加し、`Download`（installer.go）と `FetchReleases`（release.go）の両方で共有するよう修正した。
3. **`Extract`失敗時はロールバックされていなかった**: 元実装は起動確認（`VerifyLaunch`）失敗時のみ自動ロールバックしており、展開処理自体が失敗した場合（ディスクフル・権限エラー等）は `.bak` にリネーム済みの旧バージョンが復元されないまま中断していた。`Update()` の `Extract` 失敗パスにも `restoreFrom` によるロールバックを追加し、「入れ替え中に何が失敗しても自動的に旧バージョンへ戻る」という元の要求（失敗時はロールバック可能に）をより忠実に満たすようにした。2回目の Advisor 相談でこの新規分岐が未テストだと指摘され、`TestUpdate_AutoRollbackOnExtractFailure`（有効な `go/bin/go` エントリの後に `../evil.txt` を仕込んだ tar で展開を意図的に失敗させる）を追加してカバーした。

### 権限エラーの早期検出（fast-fail）

sudo なしで `goup update` を叩くと、旧実装はダウンロード（〜70MB）・sha256 検証が済んだ後の `Backup` (`os.Rename`) で初めて permission error になり、大量のネットワーク帯域を無駄にしていた。`checkWritable` ヘルパー（`os.CreateTemp` で probe ファイルを作って即削除）を追加し、`Update` では「Already up to date」判定の後・Download の前に、`Rollback` では「no backup found」判定の後に呼ぶようにした。

- **probe ファイル方式を採用した理由**: `unix.Access(dir, unix.W_OK)` は `golang.org/x/sys/unix` に依存し、CLAUDE.md の stdlib-only 方針に反する。`os.Stat` + uid 判定だと ACL / read-only mount / root squash NFS 等の実効権限を見落とす。probe ファイル作成はカーネルに実際の書き込み可否を問い合わせるため一番堅牢。
- **配置場所**: 「Already up to date」パスは `/usr/local` に一切触らないので sudo 不要のまま残したい → 判定の**後**に checkWritable を置いた。これで最新版済みなら sudo なしでも成功する挙動が保たれる。
- **テスト**: `TestUpdate_ReadOnlyRootFailsBeforeDownload` を追加。`os.Chmod(root, 0o555)` で root を read-only にした状態で `Update` を呼び、archive エンドポイントへの HTTP hits が 0 のままエラーが sudo hint 付きで返ることを確認する。

### CurrentVersion を `go env GOVERSION` からファイル直読みに変更

上記 fast-fail を仕込んだ後の実機テスト（`sudo /tmp/goup update`）で `exec: "go": executable file not found in $PATH` が発生。原因は Ubuntu の sudo が `secure_path=/usr/sbin:/usr/bin:/sbin:/bin` を強制するため、ユーザー PATH に入っている `/usr/local/go/bin` が sudo コンテキストでは消え、`exec.Command("go", ...)` が解決不能になるというもの。全 Ubuntu ユーザーが踏む本物のバグだった。

- **修正**: `CurrentVersion` を `CurrentVersion(installRoot)` に変更し、`<installRoot>/go/VERSION` の1行目を直読みするようにした。Go 公式 tarball は常に VERSION ファイルを同梱している（`go1.26.3\ntime ...` 形式）。
- **副次的な効能**: (a) go.mod の `toolchain` directive による自動 fetch の影響を受けない → 従来必要だった `GOTOOLCHAIN=local` の環境変数トリックが不要に。(b) `go` バイナリが壊れていても（rollback 直前など）installRoot のバージョンが読める。(c) サブプロセス起動が無くなり高速化。
- **却下した代案**: `filepath.Join(installRoot, "go", "bin", "go")` の絶対パスで exec するだけの最小修正。PATH 問題は解決するが、GOTOOLCHAIN 問題と「壊れた go を叩く」問題が残るため却下。
- **テスト**: 全 `TestUpdate_*` に `writeVersionMarker(t, root, "goOLD")` を追加（既に存在した helper を活用）。ビルド → vet → test すべて green、`/tmp/goup check` / `/tmp/goup update`（sudo なし fast-fail）ともに実機で挙動確認済み。

### Rollback 実機テストで浮上した UX 課題を修正

1.26.3 → 1.26.4 update → 1.26.3 rollback までを実機で通した際、以下 2 点の UX 問題が判明したため合わせて修正した。

- **Rollback 成功時に何も出力しない**: 静かに成功する挙動はユーザーが「本当に動いたか」不安になる。`Update` は "Updated: X -> Y" と出るのに対称性が無い。VerifyLaunch 成功後に `CurrentVersion(installRoot)` を再度呼び、`Rolled back to <version> (from <backup basename>)` を出力するようにした。
- **`wrapPermissionError` の sudo hint が `sudo goup update` に決め打ち**: `goup rollback` を sudo なしで叩いても "hint: rerun with sudo, e.g. `sudo goup update`" と出て、コマンド名が不正確。`wrapPermissionError` はサブコマンド文脈を知らないので、hint を `(hint: rerun with sudo)` に短縮して汎用化した（既存テストは "sudo" 部分文字列しか見ていないので影響なし）。

### v0.3.0: 対話 TTY での自動 sudo 昇格

#### 昇格判定は install/rollback は前段一択、update は pre-flight peek で例外扱い

`install <version>` と `rollback` の書き込み権限判定は「バージョン検証・backup 存在確認より前」に置いた。トレードオフは advisor 相談で明示的に確認済み:

- **利点**: 判定ロジックを CLI 層の 1 箇所に集約でき、`Install()` / `Rollback()` の内部フローに sudo 昇格を持ち込まずに済む。CI や非 TTY 環境では go.dev への API コール前に fast-fail するので帯域を無駄にしない。
- **欠点**: `goup install <typo>` / `goup install <現行バージョン>`（no-op）/ `goup rollback` (backup 無し) でも sudo プロンプトが先に出る。今までは検証エラー / no-op で sudo 不要だった。
- **判断**: いずれも低頻度ケースなので受容。Ctrl-C で抜けるコストは実害無し。

**update は例外**: PR #3 レビューで codex が指摘した通り、`goup update` の「もう最新版」ケースは**高頻度**（CI で毎日 update を叩く運用、追従目的の日次実行など）で発生する。ここで毎回 sudo プロンプトが出る/`--no-sudo` で fast-fail するのは v0.2.0 からの明確な UX 回帰。

対処: `runUpdate` の頭に `isAlreadyLatest(installRoot, baseURL)` の pre-flight peek を追加し、read-only な範囲で current == latest を判定してから `maybeElevate` を呼ぶ。既に最新なら "Already up to date" を出して exit 0、そうでなければ従来通り昇格 → `Update()` に進む。

- **エラー時は fall-through**: `isAlreadyLatest` は網羅的エラーハンドリングを持たず、`FetchReleases` の network error 等を検知すると単に false を返す。「わからないから通常経路に流す」設計で、Update 本体が本物のエラーを surface する。
- **重複 fetch のコスト**: 書き込みが必要なパスでは pre-flight で 1 回、Update 内で 1 回、sudo 後の再入で 1 回の計 3 回 `FetchReleases` が走る。ペイロード数 KB なので実害無し、Install/Rollback と実装対称性を優先しなかった理由でもある。
- **install に同じ扱いをしない理由**: `goup install <same-version>` は idempotent script 用途で発生しうるが update ほど高頻度ではなく、実際に困ったら install にも同じ pre-flight を追加する（`Install()` の signature を弄らずに済むため YAGNI で先送り）。

#### `syscall.Exec` + `os.Executable()` で PATH 剥奪を回避

v0.2.0 の実機テストで判明した「`sudo goup update` が `sudo: goup: command not found` で落ちる」（Ubuntu の `secure_path` が `~/go/bin` を落とす）問題への対処。

- **`syscall.Exec` を選んだ理由**: `exec.Command` だと goup が親プロセスとして残り、signal 転送・exit code 中継・stdio 中継を全部書く必要がある。`syscall.Exec` はプロセス置換で、そのあたりを全部 sudo に委譲できる。
- **`os.Executable()` を渡す理由**: sudo が secure_path を強制すると `argv[0]="goup"` は再度解決不能になる。絶対パスを渡せば sudo は PATH 解決を挟まないので確実。
- **無限昇格ループの防止**: `elevationDecision` は `uid == 0` を `canWrite` より前で判定する。sudo 経由で再実行されたプロセスは uid=0 なので必ず `decisionRun` を選び、write 判定に関わらず本体処理へ進む。

#### TTY 判定はハイブリッド（`/dev/tty` open + stdin ModeCharDevice）

PLAN.md は当初 `os.Stdin.Stat().Mode()&os.ModeCharDevice` 単独で TTY 判定するとしていたが、実機 smoke test で `goup update < /dev/null` が「昇格して sudo に "A terminal is required" を吐かせる」挙動を確認し、方針を見直した。

**候補 A: stdin ModeCharDevice 単独**（PLAN 当初案）: `/dev/null` が character device なので誤陽性。stdin redirect の判定に stdin だけを見るのは根本的に足りない。却下。

**候補 B: `/dev/tty` open 単独**: sudo の実挙動と一致し堅牢だが、CLAUDE.md 設計原則の「非対話環境（CI / cron / **パイプ / redirect**）→ fast-fail」のうちパイプと regular-file redirect が抜け落ちる（controlling terminal がある対話シェルから `cat foo | goup update` や `goup update < script.sh` を叩いた場合、sudo prompt が出てしまう）。CLAUDE.md の意図から外れる。

**採用: 両方を AND する** — (a) `/dev/tty` open 可能、かつ (b) stdin が character device。

- **カバー範囲**: CI / cron / detached script → (a) 落ち。pipe / regular-file redirect → (b) 落ち。対話 TTY → 両方満たして昇格へ流れる。CLAUDE.md 設計原則と 1 対 1 で一致。
- **既知の穴**: `goup update < /dev/null` は /dev/null 自体が character device なので (b) をすり抜けて昇格へ。stdlib-only 制約下では isatty ioctl 相当（`golang.org/x/term.IsTerminal`）を書かないと閉じられない。受容トレードオフとして README / CLAUDE.md に明記し、スクリプトは `--no-sudo` を明示することを推奨する。
- **`--no-sudo` の位置付け**: TTY 判定は best-effort。スクリプト・CI で確実に非対話を保証したいなら `--no-sudo` を渡すのが決定的な switch。README でもこちらをリードで案内する。
- **依存追加なし**（stdlib のみ）。

**テスト方針**: `isTTY()` 本体は環境依存なので runtime テストせず、`elevationDecision(uid, canWrite, tty, noSudo)` の純関数を 11 パターン table-driven で網羅。実際の TTY 判定・sudo 再実行は実機 smoke test にリレー。

#### `--no-sudo` フラグの居場所

`update` / `rollback` は元々フラグを取らなかったので `parseWriteFlags` を新設。`install` は既存の `parseInstallArgs` を 4 戻り値（version, pre, noSudo, err）に拡張して同居させた。

- **`update` / `rollback` は positional を拒否**: フラグ以外の引数が来たら error にする。従来は `flag.NewFlagSet` の `ExitOnError` で単に無視されていたが、`--no-sudo` を追加するタイミングで validate も厳格化した。

## 配布バイナリの stdlib 脆弱性をソーススキャンで見逃した (2026-09-18)

v0.3.0 の配布バイナリ（go1.26.4 ビルド）に到達可能な stdlib 脆弱性が 10 件あった（crypto/tls, net/http, os 等、go1.26.5〜1.26.6 で修正）。一方、手元の `govulncheck ./...` は 0 件だった。ソースモードは「いま PATH にある toolchain」の stdlib で判定するため、toolchain を更新した後では過去にビルドした成果物の状態を反映しない。

- **却下した案**: リリース前チェックをソースモードの `govulncheck ./...` のみで済ませる / go.mod の `go` directive を上げれば成果物も直るとみなす（directive は `go install` 利用者と CI の最低 toolchain を決めるだけで、手元ビルドの stdlib は PATH の `go` で決まる）
- **決め手**: `govulncheck -mode=binary dist/goup-linux-amd64` が 10 件を報告し、同時刻のソーススキャンは 0 件だった。対応として go1.27.1 で再ビルドした v0.3.1 を出し、`go` directive を 1.26.8 に、CI に `govulncheck` を追加した
- **覆す条件**: リリース成果物のビルドとバイナリスキャンを CI で自動化し、手元ビルドが配布経路から消えた場合
