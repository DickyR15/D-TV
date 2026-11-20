# 上游來源、著作權與修改聲明

D TV 是以 [Xanerix/guoapp](https://github.com/Xanerix/guoapp) 為上游基礎所進行的移植與修改版本。

## 上游專案

- 上游名稱：紅果鑑／真果鑑（guoapp）
- 上游作者／維護者：Xanerix
- 上游網址：https://github.com/Xanerix/guoapp
- 上游授權：GNU General Public License v3.0（GPL-3.0）
- 上游授權全文：見本專案根目錄的 [LICENSE](LICENSE)

上游程式碼及其原有著作權、作者聲明與授權權利仍歸原權利人所有。本專案不主張擁有上游程式碼的原始著作權。

## D TV 修改與新增內容

D TV 由 DickyR15 進行跨平台移植、繁體中文（台灣用語）調整、介面與操作流程修改、平台整合、建置流程及其他功能維護。

D TV 專案中特別新增或修改的原創內容，其著作權由相應創作者依法享有；其中由 DickyR15 創作的部分：

Copyright (C) 2026 DickyR15

本專案的上游衍生程式碼及上述修改，依 GNU GPL v3.0 條款提供。請閱讀 [LICENSE](LICENSE) 了解使用、修改及再散布條件。

## 品牌與商標

「D TV」、D TV 標誌及相關品牌識別用於識別本移植版本。GPL-3.0 不會自動授予任何人使用 D TV 品牌或標誌造成來源混淆、暗示官方背書或冒充官方版本的權利。商標及其他非著作權權利仍依適用法律及各自權利人規定。

## 第三方元件與素材

本專案包含或使用其他第三方套件、媒體解碼元件、字典及素材。各元件仍依其各自授權條款使用；請一併查閱其隨附的授權檔案及聲明。OpenCC 字典資訊見 [OPENCC-NOTICE.md](OPENCC-NOTICE.md)。

## 修改版本識別

本專案與上游原始專案並非同一發行版本。若重新散布或修改，請保留適用的著作權及授權聲明，並清楚標示修改者、修改日期及版本差異；不得將修改版誤稱為上游官方版本。

## Hongguo device-registration signature

The basic device-registration headers (`X-Khronos`, `X-Gorgon`, `X-Argus`)
in `native/core/provider_hongguo_registration_sign.go` are a Go port of the
registration-signing behavior in iDuanJu's `STV/Providers/Hongguo/HGSign.swift`
(`basicHeaders`, `xGorgon`, and `xArgus`) from
[my-name-is-alan/iDuanJu](https://github.com/my-name-is-alan/iDuanJu),
distributed under GPL-3.0. The port is adapted for D TV's native Go core and
keeps registration authentication separate from the V5 playback signer.
The full GPL-3.0 license remains in the repository's `LICENSE` file.


## Hongguo Current Player signer

The shared Go implementation in `native/core/provider_hongguo_current_player.go`,
its fixed protocol profile, and synthetic test vectors are adapted from the
Current Player implementation in [my-name-is-alan/iDuanJu](https://github.com/my-name-is-alan/iDuanJu)
(`HGCurrentPlayer.swift`, `HGCurrentPlayer.kt`, and `HGCurrentProfile.*`,
GPL-3.0). The implementation keeps the registration, query, timestamp, and
six-header signing dialect in D-TV's shared Go core so all supported platforms
use the same code.

The KS/custom transform lineage identified by that upstream project originates
from OpenHongguo commit
`05aa580a2fa914becabc5ae4be5b05539c66bda6` under Apache-2.0. The corresponding
license and upstream notice are retained in
`native/core/third_party/HGCurrentPlayer-Apache-2.0.txt` and
`native/core/third_party/HGCurrentPlayer-NOTICE.md`. The included protocol
fixtures are synthetic test vectors and do not contain captured device
credentials. Live server acceptance may change independently of this code.
