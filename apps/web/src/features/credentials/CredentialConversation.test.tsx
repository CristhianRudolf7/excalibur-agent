import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { defaultAgent, useAgents } from '@/features/agents/store'
import { rpc } from '@/lib/rpc/client'
import { CredentialConversation } from './CredentialConversation'
import { validatePanel } from '@/features/shell/panels'
const { navigate } = vi.hoisted(() => ({ navigate: vi.fn() }))
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }))
vi.mock('@/features/chat/Chat', () => ({
  Chat: ({
    agentId,
    conversation,
    credentialId,
  }: {
    agentId: string
    conversation: string
    credentialId: string
  }) => (
    <div
      data-testid="linked-chat"
      data-agent={agentId}
      data-conversation={conversation}
      data-request={credentialId}
    />
  ),
}))
const clients: QueryClient[] = []
beforeEach(async () => {
  useAgents.setState({ agents: [defaultAgent] })
  await i18n.changeLanguage('en')
})
afterEach(() => {
  cleanup()
  clients.forEach((client) => client.clear())
  vi.restoreAllMocks()
  navigate.mockClear()
})
function setup(conversation?: string) {
  const client = new QueryClient()
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <CredentialConversation
        agentId="excalibur"
        credentialId="req-1"
        conversation={conversation}
        onFile={() => {}}
      />
    </QueryClientProvider>,
  )
}
it('discovers an external conversation from the pending request and opens it focused on that card', async () => {
  const call = vi.spyOn(rpc, 'call').mockResolvedValue({
    requests: [
      {
        id: 'req-1',
        conversation_id: 'cnv_telegram_123',
        site: 'example.org',
        purpose: 'Sign in',
        fields: ['password'],
        remember_default: false,
        status: 'pending',
        created_at: '',
      },
    ],
  })
  setup()
  expect(await screen.findByTestId('linked-chat')).toHaveAttribute(
    'data-conversation',
    'cnv_telegram_123',
  )
  expect(screen.getByTestId('linked-chat')).toHaveAttribute('data-request', 'req-1')
  expect(call).toHaveBeenCalledWith(
    'credentials.pending',
    { conversation_id: '' },
    { signal: expect.any(AbortSignal) },
  )
  await waitFor(() => expect(navigate).toHaveBeenCalled())
})
it('uses an explicit conversation link without a global pending lookup', () => {
  const call = vi.spyOn(rpc, 'call')
  setup('cnv_telegram_456')
  expect(screen.getByTestId('linked-chat')).toHaveAttribute('data-conversation', 'cnv_telegram_456')
  expect(call).not.toHaveBeenCalled()
})
it('handles an expired or unknown request without opening an unrelated conversation', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({ requests: [] })
  setup()
  expect(await screen.findByText('This request is no longer pending.')).toBeVisible()
  expect(screen.queryByTestId('linked-chat')).not.toBeInTheDocument()
})
it('preserves valid request and conversation identifiers and forces chat mode', () => {
  expect(
    validatePanel({ credential: 'req-1', conversation: 'cnv_telegram_123', view: 'world' }),
  ).toMatchObject({ credential: 'req-1', conversation: 'cnv_telegram_123', view: 'chat' })
  expect(
    validatePanel({ credential: '<script>', conversation: 'cnv_telegram_123' }).credential,
  ).toBeUndefined()
  expect(validatePanel({ conversation: 'cnv_telegram_123' }).conversation).toBeUndefined()
})

it.each([true, false])(
  'selects the requesting personal agent for credential links (explicit=%s)',
  async (explicit) => {
    useAgents.setState({
      agents: [
        defaultAgent,
        {
          ...defaultAgent,
          id: 'pablito',
          name: 'Pablito',
          favorite: false,
          conversation_id: 'cnv_agent_pablito',
        },
      ],
    })
    vi.spyOn(rpc, 'call').mockResolvedValue({
      requests: [
        {
          id: 'req-1',
          conversation_id: 'cnv_agent_pablito',
          fields: ['password'],
          status: 'pending',
        },
      ],
    })
    setup(explicit ? 'cnv_agent_pablito' : undefined)
    expect(await screen.findByTestId('linked-chat')).toHaveAttribute('data-agent', 'pablito')
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith(
        expect.objectContaining({ params: { agentId: 'pablito' } }),
      ),
    )
  },
)
