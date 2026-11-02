import type { SkillGroup, SkillSummary } from '@/lib/rpc/contract'
export interface SkillFilters {
  text: string
  source: string
  state: string
}
export function filterSkills(skills: SkillSummary[], filters: SkillFilters) {
  const text = filters.text.trim().toLocaleLowerCase()
  return skills.filter(
    (skill) =>
      (!text || `${skill.name} ${skill.description}`.toLocaleLowerCase().includes(text)) &&
      (!filters.source || skill.source === filters.source) &&
      (!filters.state ||
        (filters.state === 'other'
          ? !['enabled', 'disabled'].includes(skill.state)
          : skill.state === filters.state)),
  )
}
export function groupSkills(skills: SkillSummary[], groups: SkillGroup[]) {
  const ids = [
    ...new Set([...groups.map((group) => group.id), ...skills.map((skill) => skill.group)]),
  ]
  return ids
    .map((id) => ({
      id,
      group: groups.find((group) => group.id === id),
      skills: skills.filter((skill) => skill.group === id),
    }))
    .filter((group) => group.skills.length)
}
