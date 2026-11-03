/** Browser test replacement for MediaPipe; excluded from ordinary builds. */
export const FilesetResolver = { isSimdSupported: async () => false }
export const PoseLandmarker = {
  createFromOptions: async () => ({
    detectForVideo: () => ({
      landmarks: [
        Array.from({ length: 33 }, (_, i) => ({
          x: 0.2 + i / 100,
          y: 0.5,
          z: 0,
          visibility: 0.99,
        })),
      ],
    }),
    close() {},
  }),
}
export const HandLandmarker = {
  createFromOptions: async () => ({
    detectForVideo: () => ({
      landmarks: [Array.from({ length: 21 }, () => ({ x: 0.5, y: 0.5, z: 0 }))],
      handedness: [[{ categoryName: 'Right' }]],
    }),
    close() {},
  }),
}
