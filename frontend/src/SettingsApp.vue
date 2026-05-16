<script setup>
import SettingsWindow from './components/SettingsWindow.vue'
</script>

<template>
  <div class="settings-root">
    <SettingsWindow :standalone="true" />
  </div>
</template>

<style>
html, body, #settings-app, .settings-root {
  width: 100%;
  height: 100%;
  margin: 0;
  padding: 0;
  overflow: hidden;
  /* Transparent so the CSS border-radius on .settings-win cuts through to the
     transparent OS window background, producing real rounded corners. */
  background: transparent;
}
</style>
