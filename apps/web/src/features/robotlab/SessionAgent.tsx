import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Chat } from '@/features/chat/Chat'
import { labCall, useRobotEvent } from './api'
import { Tabs } from './Tabs'
import { useLabText, type LabMessage } from './i18n'
import { ActivityImage } from './ActivityImage'
import { validLogImage } from './logic'
import { useInitialChatScroll } from './useInitialChatScroll'
import type { Session } from './types'

export function SessionAgent({
  session,
  attempt,
  time,
}: {
  session: Session
  attempt: number
  time?: number
}) {
  const { text } = useLabText()
  const [tab, setTab] = useState<'chat' | 'activity' | 'skills'>('chat')
  const chatPanel = useRef<HTMLDivElement>(null)
  useInitialChatScroll(chatPanel, tab === 'chat', session.id)
  return (
    <>
      <Tabs
        label={text('agent')}
        values={['chat', 'activity', 'skills']}
        value={tab}
        onChange={setTab}
      />
      <div ref={chatPanel} className="lab-agent-panel" hidden={tab !== 'chat'}>
        <Chat agentId="excalibur" onFile={() => {}} />
      </div>
      {tab === 'activity' && <Activity session={session} attempt={attempt} time={time} />}
      {tab === 'skills' && <Skills session={session.id} />}
    </>
  )
}
export function Activity({
  session,
  attempt,
  time,
}: {
  session: Session
  attempt: number
  time?: number
}) {
  const { text, language } = useLabText()
  const log = useQuery({
    queryKey: ['robotlab', 'log', session.id, attempt],
    queryFn: () => labCall('robotlab.session.log', { session: session.id, attempt }),
    refetchInterval: session.status === 'live' ? 2000 : false,
  })
  useRobotEvent('robotlab.log', ({ entry }) => {
    if (entry.session === session.id && entry.attempt === attempt) void log.refetch()
  })
  const entries =
    log.data?.entries.filter((entry) => time === undefined || entry.sim_time <= time) ?? []
  const skillEvents = session.skill_events.filter((event) => event.attempt === attempt)
  return (
    <div className="lab-agent-panel">
      {log.isPending && <p role="status">{text('loading')}</p>}
      {log.error && <p role="alert">{log.error.message}</p>}
      {!log.isPending && entries.length === 0 && skillEvents.length === 0 && (
        <p>{text('noActivity')}</p>
      )}
      <ol className="lab-log">
        {entries.map((entry) => (
          <li key={entry.id}>
            <strong>{text(entry.kind)}</strong>
            <time dateTime={entry.t}>
              {new Date(entry.t).toLocaleTimeString(language)} · {entry.sim_time.toFixed(2)} s
            </time>
            <p>{entry.summary}</p>
            {entry.image && validLogImage(entry.image) && (
              <ActivityImage session={session.id} path={entry.image} />
            )}
          </li>
        ))}
        {skillEvents.map((event, index) => (
          <li key={`skill-${index}`}>
            <strong>{text('skill')}</strong>
            <p>
              {event.skill} · {text(event.kind as LabMessage)} · {text('version')} {event.version}
            </p>
          </li>
        ))}
      </ol>
    </div>
  )
}
export function Skills({ session }: { session: string }) {
  const { text, language } = useLabText()
  const skills = useQuery({
    queryKey: ['robotlab', 'skills', session],
    queryFn: () => labCall('robotlab.skills.list', { session }),
    refetchInterval: 5000,
  })
  return (
    <div className="lab-agent-panel">
      {skills.isPending && <p role="status">{text('loading')}</p>}
      {skills.error && <p role="alert">{skills.error.message}</p>}
      {skills.data?.skills.length === 0 && <p>{text('noSkills')}</p>}
      <ul className="lab-skills">
        {skills.data?.skills.map((skill) => (
          <li key={skill.name}>
            <strong>{skill.name}</strong>
            <p>{skill.description}</p>
            <p>
              {text('version')}: {skill.version} · {text('success')}:{' '}
              {skill.success_rate === null
                ? text('unknown')
                : new Intl.NumberFormat(language, {
                    style: 'percent',
                    maximumFractionDigits: 0,
                  }).format(skill.success_rate)}{' '}
              · {text('uses')}: {skill.uses}
            </p>
            <span className="lab-badge">{text(skill.has_code ? 'code' : 'instructions')}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}
