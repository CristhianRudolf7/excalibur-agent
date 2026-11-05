import { Bell, ClipboardCheck, Folder, Workflow } from 'lucide-react'

export const primaryNavigation = [
  { key: 'myTasks', icon: ClipboardCheck },
  { key: 'reminders', icon: Bell },
  { key: 'projects', icon: Folder },
  { key: 'automations', icon: Workflow },
] as const
