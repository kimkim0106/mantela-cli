# mantela-cli

[Mantela](https://github.com/tkytel/mantela) で相互接続している交換局の運用者向けの CLI です。内線と接続先局の定義を1か所（TOML の局ディレクトリ）で管理し、そこから公開用の `mantela.json` と Asterisk（pjsip）の設定断片を生成します。

A CLI for operators of telephone exchanges interconnected with Mantela: manage extensions and peer stations in one place, and generate `mantela.json` and Asterisk (pjsip) configuration from it.

## 状態

設計中で、まだ使える実装はありません。

- 設計: [DESIGN.md](DESIGN.md)
- 設計どおりの例: [samples/](samples/)（局ディレクトリと、期待する生成物）

## ライセンス

[MIT](LICENSE)
