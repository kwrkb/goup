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
