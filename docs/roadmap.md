# Roadmap（路线图）

> 本文档收录当前缺失、计划中的功能与设计决策。已实现项标注 ✅，进行中 ⚙️，未开始 ⬜。
> 相关模块：`backend/internal/modules/todos`（待办 REST）、`backend/internal/modules/calendar`（CalDAV/iCal/事件）、
> `backend/internal/models/models.go`（数据模型）、`frontend/src/pages/TodosPage.tsx`（待办页）。

---

## 一、待办“清单”重构：清单 = 只有 VTODO 的日历（tasks.org 交互）✅（基础版已实现）

**现状问题**
- 待办只有两级归属：个人（`calendar="self"`）或整个团队（`calendar="team"`），外加一个自由文本
  `Group` 字段做“分组”，分组不是一个实体：不能改名、配色、删除、共享，侧栏只是从数据里现算的字符串集合。
- 待办页侧栏“新分组”按钮点开的是“新建待办”弹窗而不是“新建分组/清单”，交互误导（bug，已确认）。
- CalDAV 自定义日历（`MKCALENDAR`）全团队可见，没有“某几个成员共享”的概念。

**目标形态（对齐 tasks.org / Apple Reminders）**
1. 侧栏的“分组”全部改为**清单（List）**。
2. “新建清单”= 新建一个 **Components 只有 VTODO 的日历**（`models.Calendar`），
   CalDAV/tasks.org 客户端会把它识别为任务清单并同步；同时**事件类客户端/网页日历不会把它当作事件日历展示**
   （iCal 订阅与事件接口本就只取 self/team 的事件与待办；纯 VTODO 清单只出现在待办模块与支持 tasks 的 CalDAV 客户端）。
3. 每个待办属于一个清单（即写入对应日历名 `Todo.Calendar`），不再有自由文本“分组”的创建流程。
4. 内置清单仍然存在：
   - **个人待办**（`self`）：自己的私人待办（与私人事件共用个人日历）。
   - **团队待办**（`team`）：旧版“共享给团队”的待办，保留迁移兼容。
5. 清单可配置 **颜色 + 图标（emoji）**，共享的清单在图标右下角叠加共享角标。

**数据模型/接口（✅ 已实现）**
- `models.Calendar` 新增 `Access`：`legacy`（历史 MKCALENDAR 日历，保持全团队可见）与
  `members`（网页新建的清单：仅 owner + 共享成员可见）。
- 新增 `models.CalendarShare(TeamID, Calendar, UserID)`：成员级共享关系表。
- REST：
  - `GET    /api/v1/todo-lists`——内置 self/team + 当前用户可访问的自定义 VTODO 日历（含共享清单）。
  - `POST   /api/v1/todo-lists`——创建清单（名称/颜色/图标/共享成员）。
  - `PUT    /api/v1/todo-lists/:id`——改名/改颜色图标/重设共享成员（仅 owner）。
  - `DELETE /api/v1/todo-lists/:id`——删除清单（级联删除共享关系与清单内待办，仅 owner）。
  - `POST/PUT /api/v1/todos` 支持 `calendar` 直接指定清单；`GET` 合并个人、团队、可访问清单的待办；
    update/toggle/delete 权限改为按“清单可写性”判断。
- CalDAV：`ListCalendars/GetCalendar/GetCalendarObject/Put/Set/Delete` 对 `members` 清单做成员级过滤，
  非成员不可见不可写；`legacy` 日历维持旧行为。

**前端 ✅（已实现）**
- 待办页侧栏改为“清单”列表：个人待办、团队待办、自定义清单（颜色 + 图标 + 共享角标）。
- 新建/编辑清单弹窗（原“新分组”按钮修复）：名称、颜色、图标、共享成员多选（同团队成员）。
- 待办编辑器：删除“分组”下拉与“共享给团队”勾选，改为选择所属清单；快捷添加落到当前清单。
- 旧数据：`Todo.Group` 字段仍随行显示为标签式 chip（不再作为侧栏分组）。

**遗留问题 / 待定**
- `Todo.Group` 是否彻底废弃：可考虑一次性迁移（旧分组值并入 Tags，旧分组按“组名+所有者”自动生成清单）。
- 内置“团队待办”（`team`）长期是否保留：若所有人改用“共享清单”，可把 team 数据迁移到 owner 的共享清单并隐藏内置项。
- 历史 MKCALENDAR 的纯 VTODO 日历（`Access=legacy`）仍全员可见且不可从网页编辑；
  是否提供“转为成员共享清单”的入口（需要 owner 操作 + 迁移其中他人数据的安全校验）。

---

## 二、按团队成员共享待办清单（项目小组共享）✅（已实现）

- 共享粒度：**清单级**（不是逐条待办）；被共享的成员可在自己的待办页看到该清单并可增删改、勾选完成。
- 共享 UI：创建/编辑清单弹窗中多选团队成员；共享后清单名左侧图标右上/右下角显示共享角标
  （`.members.length > 0` 或内置团队清单）。
- 视觉区分：清单自身 **颜色 + emoji 图标**，可随时编辑（颜色/图标显示在待办页与 CalDAV 客户端）。
- 权限：成员可写清单内容；**仅 owner** 可改清单名/颜色/图标/共享成员与删除清单（CalDAV PROPPATCH 同样限制）。
- 分享角标显示规则与设计稿细节待前端完成。

---

## 三、标签与优先级过滤 ✅（已实现）

现状：待办支持 tags（逗号分隔）与 priority（1–9，高低中三档），前端只读展示，无过滤。
目标：
- 侧栏标签区改为可点击过滤：点标签仅显示带该标签的待办（可多选/再点取消）。
- 优先级过滤：工具栏下拉选择“全部 / 高 / 中 / 低”，或点击行上的 flag 图标过滤。
- 过滤与“已完成开关”、清单选择可叠加。

---

## 四、邀请成员（日历事件 + 待办）✅（已实现）

**现状问题**：网页“新建事件”没有邀请成员功能——只能改可见性（private/busy/team）。
CalDAV 侧已有 iTIP/scheduling 基础设施（`scheduling.go`：PUT 带 ATTENDEE 的事件后投递
schedule-inbox 并把事件自动复制进受邀者个人日历），但网页 REST（`/api/v1/events`）完全不读写
`CalendarEvent.Attendees`。

**统一机制：受邀成员 = 副本（copy）到其个人 `self` 日历**

网页 REST 创建的“邀请”不引入新协议，而是沿用 CalDAV 侧 `autoScheduleEvent` 的既有做法：
organizer 的行记录受邀者（`Attendees` JSON，RFC 5545 语义），同时为每位受邀成员在其
**个人 `self` 日历**创建一条私有副本（同一 UID）。这样：

- 受邀成员的 Web/CalDAV/移动端（tasks.org、Apple 日历等）都会在自己日历里看到被邀请的内容；
- 更新/删除由 organizer 触发时级联刷新/删除受邀者副本；
- 受邀者本地改动（完成/删副本/编辑）只影响自己的副本，不影响 organizer（与事件 auto-schedule 一致）。

### 四 A、日历事件邀请多个团队成员

1. `POST/PUT /api/v1/events` 增加 `attendees`（团队成员 id 数组），校验成员归属当前团队；
   行上写 `Attendees` JSON；为受邀成员在个人 `self` 日历 upsert 私有副本；移除受邀者时删除其副本；
   删除事件级联删除所有受邀副本。
2. VEVENT 序列化（CalDAV/订阅）读出/写入 `ATTENDEE;CN=…;PARTSTAT=…`，外部客户端改动（PUT 带
   ATTENDEE）也落库（不再仅靠 `deliverToInboxAndAutoSchedule` 即时处理）。
3. 前端新建/编辑事件弹窗增加“邀请成员”多选；卡片显示受邀者；成员在自己个人日历看到邀请。
4. 可选（暂不做）：忙闲冲突提示、网页端接受/拒绝收件箱 UI。

### 四 B、新建待办时邀请其他成员（复用 VTODO 自身能力）

1. `models.Todo` 增加 `Attendees` JSON 列；新建/编辑待办支持 `attendees`（成员 id 数组）。
2. **VTODO 原生字段往返**：序列化时写 `ATTENDEE;CN=…;PARTSTAT=…`；CalDAV/tasks.org PUT 回传的
   VTODO 会解析 `ATTENDEE` 落库；订阅 feed 同样输出。
3. 网页在“新建待办（带详情） / 编辑待办”时可选“邀请成员”：organizer 待办行记录受邀者，
   受邀成员的 `self` 清单获得私有副本（同一 UID），在其 Web/CalDAV 待办中可见；
   取消邀请/删除待办时级联删除副本。
4. 受邀副本保留受邀者本地的完成/进度状态，organizer 后续修改内容时不同步覆盖这些状态。

## 五. 通过 caldav 的 VJOURNAL 实现笔记模块

- 笔记和待办一样有分组，标签，邀请他人协作
- VJOURNAL笔记/VEVENT日历事件/VTODO待办 的附件都上传到 caldav 对应的team/personal 目录，并通过url 访问。
- 笔记团队实时协作编辑，用 yjs + @tiptap/extension-collaboration

### 五-A 笔记后端（VJOURNAL 行 + 清单/标签/权限）✅（后端已实现）
- 笔记 = `CalendarEvent` 行（`ComponentType=VJOURNAL`），个人在 self、团队在 team、共享笔记在
  成员级笔记清单（`Components=VJOURNAL` 的自定义日历，复用 `CalendarShare` 分享）。
- REST：`GET/POST/PUT/DELETE /api/v1/notes`、`GET/POST/PUT/DELETE /api/v1/note-lists`（列表增删改仅 owner）。
- `CalendarEvent` 增加 `Tags`（逗号分隔）列，VJOURNAL 序列化/解析为 `CATEGORIES`；
  CalDAV PUT 回传的 VJOURNAL（含 CATEGORIES）直接出现在笔记列表中。
- Web 事件列表/iCal 订阅不再混入 VJOURNAL；CalDAV 客户端仍可取到 VJOURNAL。

### 五-A2 笔记前端 ⬜（下一阶段）
- 侧栏清单 + 颜色/图标 + 共享角标（复用待办页的交互与弹窗），笔记正文编辑器。
- 团队实时协作编辑（yjs + TipTap，见五-C）在此页集成。

**风险/待定**
- 副本与 organizer 主行是两条数据：受邀者勾选完成、编辑或删除副本不会回写 organizer（如需回写，
  应改为共享清单/共享日历方案，或引入 iTIP REPLY 处理）。
- 副本一律 private、仅受邀者本人可见；不做“我创建的 vs 我受邀的”来源标记（个人日历即可区分）。
- VEVENT/VTODO 的 `ORGANIZER` 暂不持久化列（读回时仅保留 ATTENDEE）；如需完整的 organizer 语义再加列。

---

## 备注

- 前端改动后必须运行 `./build.sh`（或 `cd frontend && pnpm build`）以刷新嵌入 `homihub` 的 dist。
- 无自动化测试；`tests/test_rest_api.py` 可跑 REST 冒烟，CalDAV/litmus 为远期计划。
