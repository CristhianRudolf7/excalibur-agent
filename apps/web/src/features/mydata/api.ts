import { useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { rpc } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import { hashFile, putBlob } from '@/features/library/api'

export const dataKey = ['mydata'] as const
export const useCollections = () =>
  useQuery({
    queryKey: [...dataKey, 'collections'],
    queryFn: ({ signal }) => rpc.call('collections.list', {}, { signal }),
  })
export const useCollection = (id: string) =>
  useQuery({
    queryKey: [...dataKey, id, 'schema'],
    queryFn: ({ signal }) => rpc.call('collections.get', { id }, { signal }),
    enabled: !!id,
  })
export const useRecord = (collection: string, id: string) =>
  useQuery({
    queryKey: [...dataKey, collection, 'record', id],
    queryFn: ({ signal }) => rpc.call('records.get', { collection, id }, { signal }),
    enabled: !!id,
  })
export function useCollectionEvents() {
  const client = useQueryClient()
  useEffect(
    () =>
      subscribeEvents({
        types: ['collections.changed'],
        event: (event) => {
          if (event.type === 'collections.changed')
            void client.invalidateQueries({ queryKey: dataKey })
        },
        ready: () => {
          void client.invalidateQueries({ queryKey: dataKey })
        },
        offline: () => {},
      }),
    [client],
  )
}
export async function uploadDataFile(file: File) {
  const digest = await hashFile(file)
  await putBlob(digest, file)
  return digest
}
