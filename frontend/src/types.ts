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
  visibility: Visibility
  createdAt: string
  updatedAt: string
  start: string
  end: string
  single: boolean
  source?: 'todo'
}

export interface Todo {
  id: string
  teamId: string
  userId: string
  title: string
  note: string
  completed: boolean
  shared: boolean
  dueAt: string | null
  rrule: string
  hasDate: boolean
  createdAt: string
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
