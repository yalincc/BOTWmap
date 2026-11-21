<template>
  <span style="display: none" />
</template>

<script setup>
import { useMessage } from 'naive-ui'
import { setMessageApi } from '../composables/message'

setMessageApi(useMessage())
</script>
