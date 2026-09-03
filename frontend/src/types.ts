export interface User {
  id: string
  teamId: string
  email: string
  name: string
  role: 'parent' | 'child'
  createdAt: string
}

export interface Team {
  id: string
  name: string
  ownerId: string
  createdAt: string
}

export interface TeamEntry {
  id: string
  name: string
  role: string
  isHome: boolean
}

export interface Member {
  id: string
  email: string
  name: string
  role: string
  isOwner: boolean
  createdAt: string
}

export interface Invite {
  id: string
  role: string
  link?: string
  expiresAt: string
}

export const VisibilityPrivate = 1
export const VisibilityBusy = 2
export const VisibilityTeam = 3
export type Visibility = 1 | 2 | 3

export interface Reminder {
  unit: 'min' | 'hour' | 'day' | 'at'
  value: number
  at?: string
}

// A team member invited to an event / todo (RFC 5545 ATTENDEE semantics).
export interface Attendee {
  id: string
  name: string
  email: string
  status: string
}

export interface CalendarEvent {
  id: string
  teamId: string
  userId: string
  uid: string
  title: string
  category: string
  location: string
  description: string
  startsAt: string
  endsAt: string
  allDay: boolean
  rrule: string
  exdates: string[]
  visibility: Visibility
  createdAt: string
  updatedAt: string
  start: string
  end: string
  single: boolean
  source?: 'todo'
  reminders?: Reminder[]
  attendees?: Attendee[]
}

export interface Todo {
  id: string
  teamId: string
  userId: string
  title: string
  note: string
  completed: boolean
  calendar: string
  shared?: boolean
  dueAt: string | null
  startAt: string | null
  rrule: string
  exdates: string[]
  group: string
  tags: string
  priority: number
  location: string
  url: string
  percent: number
  parentId: string
  order: number
  hasDate: boolean
  reminders: Reminder[]
  attendees?: Attendee[]
  createdAt: string
  updatedAt: string
}

export interface TodoListMember {
  id: string
  name: string
}

// A todo list is a VTODO-only calendar: "self" (personal), "team" (built-in
// team list) or a custom calendar created from the todos page.
export interface TodoList {
  id: string
  kind: 'personal' | 'team' | 'custom'
  name: string
  color: string
  icon: string
  ownerId: string
  access: string // 'legacy' | 'members'
  canEdit: boolean
  writable: boolean
  members: TodoListMember[]
}

// A note stored as a VJOURNAL calendar object.
export interface Note {
  id: string
  uid: string
  teamId: string
  userId: string
  calendar: string
  title: string
  body: string
  tags: string
  createdAt: string
  updatedAt: string
}

// A note list (VJOURNAL-only calendar): "self", "team" or a custom list.
export interface NoteList {
  id: string
  kind: 'personal' | 'team' | 'custom'
  name: string
  color: string
  icon: string
  ownerId: string
  access: string
  canEdit: boolean
  writable: boolean
  members: TodoListMember[]
}

export interface FileItem {
  id: string
  teamId: string
  scope: 'public' | 'personal'
  ownerId: string
  name: string
  mimeType: string
  size: number
  folderId: string
  deletedAt: string | null
  createdAt: string
  updatedAt: string
}

// A file attached to an event / todo / note item. Content is served by the
// files module (url). Attachment files are hidden from the Files page listing
// and the WebDAV mount.
export interface Attachment {
  id: string
  teamId: string
  kind: 'event' | 'todo' | 'note'
  itemId: string
  fileId: string
  userId: string
  scope: 'public' | 'personal'
  name: string
  mimeType: string
  size: number
  url: string
  createdAt: string
}

// Reverse-lookup info: which event / note / todo an attachment belongs to.
export interface AttachmentItemInfo {
  id: string
  kind: 'event' | 'note' | 'todo'
  title: string
  calendar: string
  completed: boolean
  startsAt?: string
  endsAt?: string
  dueAt?: string | null
}

export interface AttachmentManageView extends Attachment {
  item: AttachmentItemInfo | null
}

export interface FileFolderItem {
  id: string
  teamId: string
  scope: 'public' | 'personal'
  ownerId: string
  parentId: string
  name: string
  deletedAt: string | null
  createdAt: string
  updatedAt: string
}

export const Categories = ['work', 'school', 'family'] as const
export type Category = (typeof Categories)[number]
