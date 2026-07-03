# Implementation notes

## バグ修正: 権限エラー時に sudo ヒントが出ていなかった

ユーザーから「sudoなしでどうなるか確認」と言われて再現しようとしたところ、`wrapPermissionError` が一度も発火していないことが判明した。原因: `os.IsPermission` は `*PathError`/`*LinkError`/`*SyscallError` を直接受け取った場合しか中身を見ない（`not all errors implementing Unwrap()` — Go標準ライブラリのコメントどおり）実装になっているが、各呼び出し箇所は `wrapPermissionError(fmt.Errorf("...: %w", err))` のように、先に `fmt.Errorf` でラップしてから渡していたため、`os.IsPermission` からは常に非該当の型に見えて `false` を返し続けていた。結果として「sudo で再実行してください」というヒントは実装上決して表示されない状態だった。

修正: `os.IsPermission(err)` を `errors.Is(err, os.ErrPermission)` に置き換えた。`errors.Is` は `Unwrap()` チェーンを再帰的に辿るため、`fmt.Errorf("%w", ...)` で何重にラップされていても正しく検出できる。`t.Chmod(root, 0o555)` で書き込み不可のディレクトリを作り実際に `Backup()` を root権限なしで実行して再現・修正確認し、回帰防止として `TestWrapPermissionError_SeesWrappedError` を `installer_test.go` に追加した。

このバグは既存の `installer_test.go` では検出できていなかった（`wrapPermissionError` 自体を直接テストしていなかったため）。今後、`fmt.Errorf` でラップしたエラーに対してエラー種別判定を行う際は `errors.Is`/`errors.As` を使い、`os.IsPermission` 等のレガシーな型アサーションベースの判定関数をラップ後のエラーに対して使わないよう注意する。

## テスト容易性のための installRoot/baseURL 引数化

`installer.go` の各関数は `installRoot`（デフォルト `/usr/local`）と `Update`/`FetchReleases` の `baseURL`（デフォルト `https://go.dev/dl`）を引数として受け取る設計にした。理由: 実際の `/usr/local/go` を書き換えずに、sha256検証失敗時に展開へ進まないこと・起動確認失敗時に自動ロールバックすること・バックアップが最新1世代のみ残ることを `go test` で自動証明する唯一の方法だったため。`installer_test.go` は `httptest.Server` でダミーの go.dev/dl JSON とダミー tar.gz を返し、`t.TempDir()` を install root として渡して検証する。

## CurrentVersion() に GOTOOLCHAIN=local を付与

ユーザーからのフォローアップ（go.mod の toolchain directive との連携について）を受け、`exec.Command("go", "env", "GOVERSION")` 実行時に `GOTOOLCHAIN=local` を環境変数へ追加した。理由: Go 1.21+ では実行時カレントディレクトリの `go.mod` に `toolchain` 指定があると `go` コマンドが別バージョンを自動解決して報告することがあり、`/usr/local/go` の実体とズレる可能性があったため。`go.mod` 側の toolchain directive 自体の書き換えはスコープ外（README に明記）。

## Backup の世代管理を「新規バックアップ作成前に旧バックアップを削除」で実装

元仕様の「世代管理は最新1世代のみでよい」を、`Backup()` の先頭で既存の `go.bak.*` を削除してから新しいバックアップへ rename する形で実装した。ロールバック可能な状態は常に「直前の1世代」のみで、複数世代の同時保持はしない。

## サブコマンド dispatch は flag.NewFlagSet を各コマンドで生成

現時点でどのサブコマンドもオプションフラグを持たないが、CLAUDE.md の「サブコマンドの dispatch は標準 flag パッケージで実装する」という規約を明示的に満たすため、`os.Args[1]` で分岐した後に `flag.NewFlagSet(name, flag.ExitOnError).Parse(os.Args[2:])` を挟んだ。将来フラグを追加する際の自然な拡張点にもなる。

## tar 展開時の zip-slip 対策

`archive/tar` の展開で、各エントリの結合後パスが `installRoot` 配下であることを確認し、外れる場合はエラーにして展開を中断する（`installer_test.go` の `TestExtract_RejectsPathTraversal` で検証）。go.dev の公式 tarball は信頼できるソースだが、sha256 検証をすり抜けた場合や将来の入力元変更に備えた防御的実装。

## goup update / goup rollback の実機（実際の /usr/local/go 入れ替え）テストは未実施

`sudo` 権限が必要かつ開発環境の Go インストールに直接影響するため、ユーザーの明示的な許可なしには実行しなかった。`goup check` のみ実機（このWSL2環境）で動作確認済み（go1.26.4 = 最新安定版のため "Up to date." を正しく表示）。`update`/`rollback` の中核ロジックは `installer_test.go` の httptest + t.TempDir によるテストで検証している。

## Windows 非対応ガードの検証範囲

`runtime.GOOS == "windows"` の分岐は `GOOS=windows GOARCH=amd64 go build` でのクロスコンパイルが通ることを確認したのみで、実際に Windows / Wine 上で実行してのクラッシュしないことの確認はしていない（このLinux環境では実行できないため）。ロジック自体は単純な文字列比較と `os.Exit(1)` のみで、クラッシュする要素はないと判断した。

## Advisor 指摘を受けた追加修正

一通り実装・テストが揃った時点で Advisor に相談したところ、以下の指摘を受け、いずれも反映した。

1. **`Extract` が実際の go.dev tarball で検証されていない**: `installer_test.go` は合成した1ファイルのみの tar.gz しか使っておらず、実際の配布物に含まれるかもしれない symlink / hardlink / pax header を `switch` の `default` で黙って無視している可能性があった。`installer_manual_test.go`（ビルドタグ `manual` で通常の `go test`/CI からは除外）を追加し、実際に go.dev から現行の linux/amd64 tarball をダウンロードして `Extract` に通し、展開結果の `go version` と `go build` が動くことを確認した（`go test -tags manual -run TestRealArchiveExtracts -v ./...`）。結果: 現行の公式 tarball は symlink 等を含まず、既存の実装で問題なく展開・起動・ビルドできることを実機で確認済み。
2. **ダウンロードにタイムアウトが無かった**: `http.Get` を直接使っていたため、go.dev への接続がハングすると無期限に待ち続ける可能性があった。`installer.go` に `httpClient = &http.Client{Timeout: 5 * time.Minute}` を追加し、`Download`（installer.go）と `FetchReleases`（release.go）の両方で共有するよう修正した。
3. **`Extract`失敗時はロールバックされていなかった**: 元実装は起動確認（`VerifyLaunch`）失敗時のみ自動ロールバックしており、展開処理自体が失敗した場合（ディスクフル・権限エラー等）は `.bak` にリネーム済みの旧バージョンが復元されないまま中断していた。`Update()` の `Extract` 失敗パスにも `restoreFrom` によるロールバックを追加し、「入れ替え中に何が失敗しても自動的に旧バージョンへ戻る」という元の要求（失敗時はロールバック可能に）をより忠実に満たすようにした。2回目の Advisor 相談でこの新規分岐が未テストだと指摘され、`TestUpdate_AutoRollbackOnExtractFailure`（有効な `go/bin/go` エントリの後に `../evil.txt` を仕込んだ tar で展開を意図的に失敗させる）を追加してカバーした。

## 権限エラーの早期検出（fast-fail）

sudo なしで `goup update` を叩くと、旧実装はダウンロード（〜70MB）・sha256 検証が済んだ後の `Backup` (`os.Rename`) で初めて permission error になり、大量のネットワーク帯域を無駄にしていた。`checkWritable` ヘルパー（`os.CreateTemp` で probe ファイルを作って即削除）を追加し、`Update` では「Already up to date」判定の後・Download の前に、`Rollback` では「no backup found」判定の後に呼ぶようにした。

- **probe ファイル方式を採用した理由**: `unix.Access(dir, unix.W_OK)` は `golang.org/x/sys/unix` に依存し、CLAUDE.md の stdlib-only 方針に反する。`os.Stat` + uid 判定だと ACL / read-only mount / root squash NFS 等の実効権限を見落とす。probe ファイル作成はカーネルに実際の書き込み可否を問い合わせるため一番堅牢。
- **配置場所**: 「Already up to date」パスは `/usr/local` に一切触らないので sudo 不要のまま残したい → 判定の**後**に checkWritable を置いた。これで最新版済みなら sudo なしでも成功する挙動が保たれる。
- **テスト**: `TestUpdate_ReadOnlyRootFailsBeforeDownload` を追加。`os.Chmod(root, 0o555)` で root を read-only にした状態で `Update` を呼び、archive エンドポイントへの HTTP hits が 0 のままエラーが sudo hint 付きで返ることを確認する。

## CurrentVersion を `go env GOVERSION` からファイル直読みに変更

上記 fast-fail を仕込んだ後の実機テスト（`sudo /tmp/goup update`）で `exec: "go": executable file not found in $PATH` が発生。原因は Ubuntu の sudo が `secure_path=/usr/sbin:/usr/bin:/sbin:/bin` を強制するため、ユーザー PATH に入っている `/usr/local/go/bin` が sudo コンテキストでは消え、`exec.Command("go", ...)` が解決不能になるというもの。全 Ubuntu ユーザーが踏む本物のバグだった。

- **修正**: `CurrentVersion` を `CurrentVersion(installRoot)` に変更し、`<installRoot>/go/VERSION` の1行目を直読みするようにした。Go 公式 tarball は常に VERSION ファイルを同梱している（`go1.26.3\ntime ...` 形式）。
- **副次的な効能**: (a) go.mod の `toolchain` directive による自動 fetch の影響を受けない → 従来必要だった `GOTOOLCHAIN=local` の環境変数トリックが不要に。(b) `go` バイナリが壊れていても（rollback 直前など）installRoot のバージョンが読める。(c) サブプロセス起動が無くなり高速化。
- **却下した代案**: `filepath.Join(installRoot, "go", "bin", "go")` の絶対パスで exec するだけの最小修正。PATH 問題は解決するが、GOTOOLCHAIN 問題と「壊れた go を叩く」問題が残るため却下。
- **テスト**: 全 `TestUpdate_*` に `writeVersionMarker(t, root, "goOLD")` を追加（既に存在した helper を活用）。ビルド → vet → test すべて green、`/tmp/goup check` / `/tmp/goup update`（sudo なし fast-fail）ともに実機で挙動確認済み。

## Rollback 実機テストで浮上した UX 課題を修正

1.26.3 → 1.26.4 update → 1.26.3 rollback までを実機で通した際、以下 2 点の UX 問題が判明したため合わせて修正した。

- **Rollback 成功時に何も出力しない**: 静かに成功する挙動はユーザーが「本当に動いたか」不安になる。`Update` は "Updated: X -> Y" と出るのに対称性が無い。VerifyLaunch 成功後に `CurrentVersion(installRoot)` を再度呼び、`Rolled back to <version> (from <backup basename>)` を出力するようにした。
- **`wrapPermissionError` の sudo hint が `sudo goup update` に決め打ち**: `goup rollback` を sudo なしで叩いても "hint: rerun with sudo, e.g. `sudo goup update`" と出て、コマンド名が不正確。`wrapPermissionError` はサブコマンド文脈を知らないので、hint を `(hint: rerun with sudo)` に短縮して汎用化した（既存テストは "sudo" 部分文字列しか見ていないので影響なし）。

## v0.3.0: 対話 TTY での自動 sudo 昇格

### 昇格判定は install 前段（FetchAllReleases より前）で実行する

`install <version>` の書き込み権限判定を「バージョン検証後」ではなく「コマンド起動直後」に置いた。トレードオフは advisor 相談で明示的に確認済み:

- **利点**: 判定ロジックを CLI 層の 1 箇所（`main.go` の `runUpdate`/`runInstall`/`runRollback`）に集約でき、`Install()` の内部フローに sudo 昇格を持ち込まずに済む。CI や非 TTY 環境では go.dev への API コール前に fast-fail するので帯域を無駄にしない。
- **欠点**: `goup install <typo>` や `goup install <現行バージョン>`（no-op）でも sudo プロンプトが先に出る。今までは検証エラー / no-op で sudo 不要だった。
- **判断**: CLI 層で 1 箇所に集約するメリット（テスト容易性・実装の単純さ）を優先。sudo プロンプトが不要と判明するのはレアケース（typo か no-op のみ）で、Ctrl-C で抜けるコストは実害無し。将来ユーザーからの苦情が出たら「バージョン検証だけ先にやってから昇格」への分割を検討する。

### `syscall.Exec` + `os.Executable()` で PATH 剥奪を回避

v0.2.0 の実機テストで判明した「`sudo goup update` が `sudo: goup: command not found` で落ちる」（Ubuntu の `secure_path` が `~/go/bin` を落とす）問題への対処。

- **`syscall.Exec` を選んだ理由**: `exec.Command` だと goup が親プロセスとして残り、signal 転送・exit code 中継・stdio 中継を全部書く必要がある。`syscall.Exec` はプロセス置換で、そのあたりを全部 sudo に委譲できる。
- **`os.Executable()` を渡す理由**: sudo が secure_path を強制すると `argv[0]="goup"` は再度解決不能になる。絶対パスを渡せば sudo は PATH 解決を挟まないので確実。
- **無限昇格ループの防止**: `elevationDecision` は `uid == 0` を `canWrite` より前で判定する。sudo 経由で再実行されたプロセスは uid=0 なので必ず `decisionRun` を選び、write 判定に関わらず本体処理へ進む。

### TTY 判定はハイブリッド（`/dev/tty` open + stdin ModeCharDevice）

PLAN.md は当初 `os.Stdin.Stat().Mode()&os.ModeCharDevice` 単独で TTY 判定するとしていたが、実機 smoke test で `goup update < /dev/null` が「昇格して sudo に "A terminal is required" を吐かせる」挙動を確認し、方針を見直した。

**候補 A: stdin ModeCharDevice 単独**（PLAN 当初案）: `/dev/null` が character device なので誤陽性。stdin redirect の判定に stdin だけを見るのは根本的に足りない。却下。

**候補 B: `/dev/tty` open 単独**: sudo の実挙動と一致し堅牢だが、CLAUDE.md 設計原則の「非対話環境（CI / cron / **パイプ / redirect**）→ fast-fail」のうちパイプと regular-file redirect が抜け落ちる（controlling terminal がある対話シェルから `cat foo | goup update` や `goup update < script.sh` を叩いた場合、sudo prompt が出てしまう）。CLAUDE.md の意図から外れる。

**採用: 両方を AND する** — (a) `/dev/tty` open 可能、かつ (b) stdin が character device。

- **カバー範囲**: CI / cron / detached script → (a) 落ち。pipe / regular-file redirect → (b) 落ち。対話 TTY → 両方満たして昇格へ流れる。CLAUDE.md 設計原則と 1 対 1 で一致。
- **既知の穴**: `goup update < /dev/null` は /dev/null 自体が character device なので (b) をすり抜けて昇格へ。stdlib-only 制約下では isatty ioctl 相当（`golang.org/x/term.IsTerminal`）を書かないと閉じられない。受容トレードオフとして README / CLAUDE.md に明記し、スクリプトは `--no-sudo` を明示することを推奨する。
- **`--no-sudo` の位置付け**: TTY 判定は best-effort。スクリプト・CI で確実に非対話を保証したいなら `--no-sudo` を渡すのが決定的な switch。README でもこちらをリードで案内する。
- **依存追加なし**（stdlib のみ）。

**テスト方針**: `isTTY()` 本体は環境依存なので runtime テストせず、`elevationDecision(uid, canWrite, tty, noSudo)` の純関数を 11 パターン table-driven で網羅。実際の TTY 判定・sudo 再実行は実機 smoke test にリレー。

### `--no-sudo` フラグの居場所

`update` / `rollback` は元々フラグを取らなかったので `parseWriteFlags` を新設。`install` は既存の `parseInstallArgs` を 4 戻り値（version, pre, noSudo, err）に拡張して同居させた。

- **`update` / `rollback` は positional を拒否**: フラグ以外の引数が来たら error にする。従来は `flag.NewFlagSet` の `ExitOnError` で単に無視されていたが、`--no-sudo` を追加するタイミングで validate も厳格化した。
