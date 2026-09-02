# CalDAV 兼容性 TODO

按主流客户端（Apple Calendar / iOS 日历、Google Calendar、Outlook via CalDAV Synchronizer、Nextcloud）的依赖程度排序。

## 核心 / 极高优先级

### 1. sync-token（RFC 6578 Collection Synchronization）

- **状态**：✅ 已实现（`backend/internal/modules/calendar/sync.go`）
- **影响**：Apple Calendar、Nextcloud、Google、Outlook 全线原生支持并强依赖。缺少则客户端无法增量同步，只能全量拉取，事件多时延迟、流量、CPU 与移动端电量消耗都极高。
- **验证**：PROPFIND 返回 200 sync-token；REPORT sync-collection 返回增量。已从 `tests/test_caldav.py` allowlist 移除。

### 2. save-load.event.recurrences.exception（Master + Exception 单 .ics）

- **状态**：⏳ 待实现
- **标准**：RFC 4791 §4.1 — 相同 UID 的主重复事件（Master）与例外（Exception，如"把本周三的例会改到周四"）必须存放在同一个 .ics 资源中。
- **影响**：Apple Calendar / Thunderbird 编辑"仅修改此循环事件的单次实例"时会提交含 `RECURRENCE-ID` 的 .ics。若拆成独立 blob 存储，客户端更新/删除主事件或重新拉取时，修改过的单次实例会丢失、重复或时间对不上。
- **验收**：caldav-server-tester `save-load.event.recurrences.exception` 通过。

## 中高优先级

### 3. search.text.case-insensitive（大小写不敏感搜索）

- **状态**：⏳ 待实现
- **标准**：RFC 4791 `i;ascii-casemap`（协议允许大小写敏感，但主流客户端默认不敏感）。
- **影响**：Apple Calendar、Outlook、Nextcloud 搜索框默认大小写不敏感。不实现则搜索 "Meeting" 搜不到标题为 "meeting" 的日程，体验像系统 Bug。
- **实现思路**：文本匹配过滤时做小写转换，改动成本低。
- **验收**：caldav-server-tester `search.text.case-insensitive` 通过。

## 中低优先级（可选，后续考虑）

- `freebusy-query` — 企业/团队会议预约场景需要"查看同事此时段是否有空"。
- `scheduling.*`（RFC 6638）— 会议邀请、接受/拒绝并自动同步 RSVP 到对方日历。
- `search.time-range.alarm` — 主流客户端通常本地触发 Alert，极少依赖服务端过滤。

## 低优先级（维持设计决策）

- `create-calendar` — 预先为用户创建默认日历（auto-create），Apple/Google 托管服务同样如此，接受。
- `save-load.journal` — VJOURNAL 在现代日历客户端已基本弃用，接受。
- `principal-search` — 仅用于查找其他用户 Principal，单用户/家庭私有部署不影响，接受。
