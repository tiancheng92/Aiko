/**
 * usePetRenderer — shared imperative API contract for pet renderer components.
 *
 * Both Live2DPet.vue and VRMPet.vue expose the following via defineExpose:
 *
 *   setState(state: 'idle'|'thinking'|'speaking'|'listening'|'error'): void
 *     Drive state-dependent animations and expressions.
 *
 *   focusGlobal(x: number, y: number): void
 *     Global screen coordinates → eye/head tracking.
 *
 *   applyEmotion({ emotion: string, intensity: number }): void
 *     emotion ∈ {joy, sad, surprised, angry, neutral}; intensity ∈ [0, 1].
 *     Live2D: maps to nearest preset expression.
 *     VRM: lerps blendshape weights.
 *
 *   setSize(n: number): void
 *     Resize canvas/renderer to n×n pixels.
 */
// No runtime exports — this file documents the contract only.
export {}
