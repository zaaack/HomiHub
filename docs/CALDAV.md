# CalDAV 兼容性 TODO

按主流客户端（Apple Calendar / iOS 日历、Google Calendar、Outlook via CalDAV Synchronizer、Nextcloud）的依赖程度排序。

## 核心 / 极高优先级

### 1. sync-token（RFC 6578 Collection Synchronization）

- **状态**：✅ 已实现（`backend/internal/modules/calendar/sync.go`）
- **影响**：Apple Calendar、Nextcloud、Google、Outlook 全线原生支持并强依赖。缺少则客户端无法增量同步，只能全量拉取，事件多时延迟、流量、CPU 与移动端电量消耗都极高。
- **验证**：PROPFIND 返回 200 sync-token；REPORT sync-collection 返回增量。已从 `tests/test_caldav.py` allowlist 移除。

### 2. save-load.event.recurrences.exception（Master + Exception 单 .ics）

- **状态**：✅ 已实现（`backend/internal/modules/calendar/caldav.go`）
- **标准**：RFC 4791 §4.1 — 相同 UID 的主重复事件（Master）与例外（Exception，如"把本周三的例会改到周四"）必须存放在同一个 .ics 资源中。
- **影响**：Apple Calendar / Thunderbird 编辑"仅修改此循环事件的单次实例"时会提交含 `RECURRENCE-ID` 的 .ics。若拆成独立 blob 存储，客户端更新/删除主事件或重新拉取时，修改过的单次实例会丢失、重复或时间对不上。
- **实现**：`PutCalendarObject` 解析 multi-VEVENT payload，master 与各 exception 按 UID+RID 分库存储，随主链一致性协调（不在 payload 的旧 exception 删除）；读路径 `bundleEvents`/`toObject` 按 UID 归组，返回单个含全部 VEVENT 的资源。
- **验证**：caldav-server-tester `save-load.event.recurrences.exception` 通过（已从 allowlist 移除）。

## 中高优先级

### 3. search.text.case-insensitive（大小写不敏感搜索）

- **状态**：✅ 已实现（`backend/internal/modules/calendar/filter.go` + `vendor/go-webdav` 本地 fork）
- **标准**：RFC 4791 `i;ascii-casemap`（协议允许大小写敏感，但主流客户端默认不敏感）。
- **影响**：Apple Calendar、Outlook、Nextcloud 搜索框默认大小写不敏感。不实现则搜索 "Meeting" 搜不到标题为 "meeting" 的日程，体验像系统 Bug。
- **实现**：上游 go-webdav 在解码 REPORT 时丢弃了 `text-match` 的 `collation` 属性。项目在 `vendor/go-webdav`（本地 fork，已被 gitignore）给 `caldav.TextMatch` 增加 `Collation` 字段并在 `decodePropFilter`/`decodeParamFilter` 保留；`matchCITextMatch` 据此在 `i;octet`（大小写敏感，默认）与 `i;ascii-casemap`（大小写不敏感）间切换。
- **验证**：caldav-server-tester `search.text.case-insensitive` 通过（已从 allowlist 移除）。
- **注意**：若需同步上游，从 `<go module cache>/github.com/emersion/go-webdav@v0.7.0` 对照复制三个文件即可复现 fork 补丁。

## 中低优先级（部分已实现）

### 4. search.is-not-defined（RFC 4791 §9.7.4，is-not-defined 属性过滤）

- **状态**：✅ 已实现（`backend/internal/modules/calendar/filter.go`）
- **内容**：category / class / dtend 三个子项全部通过。
- **关键修复**：
  1. `matchCIPropFilter` 当属性存在且 `IsNotDefined` 时漏了提前 return false（走到 `return true`），导致带 CATEGORIES/CLASS/DTEND 的事件也被当成"未定义"。
  2. 存储丢失信息——`ParseEvents` 给所有事件默认 `Category:"family"`，DB `default:family`，导致无 CATEGORIES 的事件 round-trip 后也被写 `CATEGORIES:family`；CLASS 属性从未解析/重建；DURATION 被折成 DTEND。修复：`Category` 去默认值、model 新增 `Class` 字段、新增 `Duration` 字段（有 DURATION 时输出 DURATION 而非 DTEND）。
- **验证**：caldav-server-tester `search.is-not-defined.*` 通过（已从 allowlist 移除）。

### 5. search.time-range.alarm（VALARM 触发时间搜索）

- **状态**：✅ 已实现（`backend/internal/modules/calendar/caldav.go` + `filter.go`）
- **修复**：`queryRange` 不再把 VALARM comp-filter 的 time-range 泄漏进 DB 层事件过滤；`withoutTimeRange` 保留 VALARM 的 time-range；`matchCIAlarmRange` 按父事件 DTSTART 计算相对 TRIGGER、与 range 比较（绝对 TRIGGER 直接比较）。
- **验证**：caldav-server-tester `search.time-range.alarm` 通过（已从 allowlist 移除）。

### 6. search.recurrences.includes-implicit.todo（VTODO 隐式循环实例）

- **状态**：✅ 已实现（`backend/internal/modules/calendar/caldav.go` `todoInRange`）
- **修复**：`todoInRange` 原先不认 RRULE，只按首实例的 DTSTART/DUE 判 overlap；带 RRULE 的 VTODO 用 `parseRuleWithStart` 展开后判 range 内是否有实例。
- **验证**：caldav-server-tester `search.recurrences.includes-implicit.todo` 通过（已从 allowlist 移除）。

## 剩余偏离（继续接受为设计决策/后续考虑）

- `freebusy-query` — 企业/团队会议预约场景需要"查看同事此时段是否有空"，go-webdav 无 REPORT 钩子，需 fork 扩展。
- `principal-search` — 客户端"在服务器上找人/添加参与者"的发现机制，需 fork 扩展 principal-property-search REPORT。
- `scheduling.*`（RFC 6638）— 会议邀请、接受/拒绝并自动同步 RSVP 到对方日历，巨型功能。
- `search.recurrences.expanded` — REPORT 服务端展开循环实例为独立 VEVENT（python-caldav 可客户端展开）。
- `search.comp-type.optional` — tester 源码自标记 inconclusive（其 TODO）。
- `create-calendar` — 预先为用户创建默认日历（auto-create），Apple/Google 托管服务同样如此，接受。
- `save-load.journal` — VJOURNAL 在现代日历客户端已基本弃用，接受。
