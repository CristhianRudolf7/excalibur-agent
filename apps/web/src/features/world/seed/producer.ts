import { createSimulation } from '../simulation'
import { makeTimeline, LOOP_DURATION } from './timeline'
/** Mock-only producer factory; replace this factory with a live adapter without changing the engine or views. */
export const createMockProducer = (elapsed: number, epoch: number, seed = 0) =>
  createSimulation(makeTimeline(seed), LOOP_DURATION, epoch, elapsed)
