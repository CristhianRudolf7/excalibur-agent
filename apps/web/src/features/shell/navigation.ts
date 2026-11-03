import { Bell, ClipboardCheck, Folder, Globe, Workflow } from 'lucide-react'

export const primaryNavigation = [
  { key: 'myTasks', icon: ClipboardCheck },
  { key: 'reminders', icon: Bell },
  { key: 'projects', icon: Folder },
  { key: 'world', icon: Globe },
  { key: 'automations', icon: Workflow },
] as const
