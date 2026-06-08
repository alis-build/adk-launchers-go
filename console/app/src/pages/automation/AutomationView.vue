<template>
  <v-container class="automation-page py-6 py-md-10">
    <v-row justify="center">
      <v-col
        cols="12"
        xl="10"
        :class="{ 'automation-page--dark': isDarkTheme }"
      >
        <v-row class="mb-8 mb-md-12">
          <v-col
            cols="12"
            md="8"
          >
            <div class="d-inline-flex align-center mb-4 text-title-medium text-medium-emphasis">
              <v-icon
                size="18"
                class="mr-2"
              >
                history_toggle_off
              </v-icon>
              Automation
            </div>

            <div class="text-display-small automation-title mb-2">Scheduled actions manager</div>

            <div class="text-body-large text-medium-emphasis mb-0">Stay up to date on things that you care about and put your agent to work while you're away. When a scheduled action is ready, it'll show in your threads.</div>
          </v-col>

          <v-col
            cols="12"
            md="4"
          >
            <v-card
              rounded="xl"
              variant="flat"
              class="summary-card h-100"
            >
              <v-card-text class="pa-6">
                <div class="text-label-large text-medium-emphasis mb-3">Overview</div>
                <div class="text-display-medium mb-2">{{ cronCount }}</div>
                <div class="text-body-large text-medium-emphasis">Saved automations</div>
              </v-card-text>
            </v-card>
          </v-col>
        </v-row>

        <v-card
          rounded="xl"
          variant="flat"
          class="automation-panel"
        >
          <v-card-text class="pa-4 pa-md-6">
            <div class="d-flex flex-column flex-md-row align-md-center justify-space-between ga-4 mb-5">
              <div>
                <div class="text-headline-small mb-2">My actions</div>
                <div class="text-body-large text-medium-emphasis">Your saved schedules will appear here once you create them.</div>
              </div>

              <div class="actions-toolbar">
                <v-btn
                  color="primary"
                  rounded="pill"
                  size="large"
                  variant="flat"
                  class="text-none"
                  @click="createDialogOpen = true"
                >
                  <v-icon
                    start
                    size="18"
                  >
                    add
                  </v-icon>
                  New schedule
                </v-btn>

                <v-btn
                  rounded="pill"
                  variant="text"
                  class="text-none"
                  :loading="loading"
                  @click="loadCrons"
                >
                  <v-icon
                    start
                    size="18"
                  >
                    refresh
                  </v-icon>
                  Refresh
                </v-btn>
              </div>
            </div>

            <v-alert
              v-if="error"
              type="error"
              variant="tonal"
              rounded="xl"
              class="mb-4"
            >
              {{ error }}
            </v-alert>

            <div
              v-if="loading"
              class="loading-grid"
            >
              <v-skeleton-loader
                v-for="n in 3"
                :key="n"
                type="article"
                class="loading-card"
              />
            </div>

            <v-card
              v-else-if="!crons.length"
              rounded="xl"
              variant="tonal"
              class="empty-state"
            >
              <v-card-text class="pa-5 pa-md-7">
                <div class="d-flex flex-column flex-md-row align-md-center ga-5">
                  <div class="empty-state__icon-wrap">
                    <v-icon size="28">history_toggle_off</v-icon>
                  </div>

                  <div class="flex-1-1">
                    <div class="text-title-large mb-2">You haven't scheduled anything yet</div>
                    <div class="text-body-large text-medium-emphasis">Create a recurring or once off schedule for summaries, reminders, or follow-ups and it will appear here.</div>
                  </div>

                  <div class="empty-state__actions">
                    <v-btn
                      color="primary"
                      rounded="pill"
                      size="large"
                      variant="flat"
                      class="text-none"
                      @click="createDialogOpen = true"
                    >
                      <v-icon
                        start
                        size="18"
                      >
                        add
                      </v-icon>
                      New schedule
                    </v-btn>
                  </div>
                </div>
              </v-card-text>
            </v-card>

            <div
              v-else
              class="cron-sections"
            >
              <div v-if="activeCrons.length">
                <div class="cron-grid">
                  <v-card
                    v-for="cron in activeCrons"
                    :key="cron.name"
                    rounded="xl"
                    variant="flat"
                    class="cron-card"
                  >
                    <v-card-text class="pa-5">
                      <div class="cron-card__header">
                        <div class="cron-card__title-wrap">
                          <div class="cron-card__chips">
                            <v-chip
                              size="small"
                              variant="tonal"
                              rounded="pill"
                            >
                              {{ cronTypeLabel(cron) }}
                            </v-chip>
                          </div>
                        </div>

                        <div class="cron-card__actions">
                          <v-btn
                            size="small"
                            variant="text"
                            class="text-none"
                            :loading="runningCronNames.has(cron.name)"
                            @click="handleRunCron(cron)"
                          >
                            <v-icon
                              start
                              size="18"
                            >
                              play_arrow
                            </v-icon>
                            Run now
                          </v-btn>

                          <v-btn
                            size="small"
                            color="error"
                            variant="text"
                            class="text-none"
                            :loading="deletingCronNames.has(cron.name)"
                            @click="handleDeleteCron(cron)"
                          >
                            <v-icon
                              start
                              size="18"
                            >
                              delete
                            </v-icon>
                            Delete
                          </v-btn>
                        </div>
                      </div>

                      <div
                        v-if="cron.initialPrompt"
                        class="cron-card__initial-prompt"
                      >
                        <div class="text-label-medium text-medium-emphasis mb-1">Initial prompt</div>
                        <div class="text-body-medium">
                          {{ cron.initialPrompt }}
                        </div>
                      </div>

                      <div class="cron-card__prompt">
                        <div class="text-label-medium text-medium-emphasis mb-1">Prompt</div>
                        <div class="text-body-medium">
                          {{ cron.prompt }}
                        </div>
                      </div>

                      <div class="cron-card__meta">
                        <div>
                          <div class="text-label-medium text-medium-emphasis mb-1">Schedule</div>
                          <div class="text-body-medium">{{ cronScheduleCopy(cron) }}</div>
                        </div>

                        <div v-if="cron.updateTime || cron.createTime">
                          <div class="text-label-medium text-medium-emphasis mb-1">Updated</div>
                          <div class="text-body-medium">{{ formatTimestampLike(cron.updateTime ?? cron.createTime) }}</div>
                        </div>

                        <div v-if="cron.lastRunTime">
                          <div class="text-label-medium text-medium-emphasis mb-1">Last run</div>
                          <div class="text-body-medium">{{ formatTimestampLike(cron.lastRunTime) }}</div>
                        </div>
                      </div>
                    </v-card-text>
                  </v-card>
                </div>
              </div>

              <div
                v-if="archivedCrons.length"
                class="cron-section--archived"
              >
                <div class="mb-4">
                  <div class="text-title-medium">Archived</div>
                  <div class="text-body-medium text-medium-emphasis">Completed or archived schedules are kept here for reference and are shown with lower priority.</div>
                </div>

                <div class="cron-grid cron-grid--archived">
                  <v-card
                    v-for="cron in archivedCrons"
                    :key="cron.name"
                    rounded="xl"
                    variant="flat"
                    class="cron-card cron-card--archived"
                  >
                    <v-card-text class="pa-5">
                      <div class="cron-card__header">
                        <div class="cron-card__title-wrap">
                          <div class="cron-card__chips">
                            <v-chip
                              size="small"
                              variant="tonal"
                              rounded="pill"
                            >
                              {{ cronTypeLabel(cron) }}
                            </v-chip>
                            <v-chip
                              size="small"
                              color="secondary"
                              variant="outlined"
                              rounded="pill"
                            >
                              Archived
                            </v-chip>
                          </div>
                        </div>

                        <div class="cron-card__actions cron-card__actions--archived">
                          <v-btn
                            size="small"
                            color="error"
                            variant="text"
                            class="text-none"
                            :loading="deletingCronNames.has(cron.name)"
                            @click="handleDeleteCron(cron)"
                          >
                            <v-icon
                              start
                              size="18"
                            >
                              delete
                            </v-icon>
                            Delete
                          </v-btn>
                        </div>
                      </div>

                      <div
                        v-if="cron.initialPrompt"
                        class="cron-card__initial-prompt"
                      >
                        <div class="text-label-medium text-medium-emphasis mb-1">Initial prompt</div>
                        <div class="text-body-medium">
                          {{ cron.initialPrompt }}
                        </div>
                      </div>

                      <div class="cron-card__prompt">
                        <div class="text-label-medium text-medium-emphasis mb-1">Prompt</div>
                        <div class="text-body-medium">
                          {{ cron.prompt }}
                        </div>
                      </div>

                      <div class="cron-card__meta">
                        <div>
                          <div class="text-label-medium text-medium-emphasis mb-1">Schedule</div>
                          <div class="text-body-medium">{{ cronScheduleCopy(cron) }}</div>
                        </div>

                        <div v-if="cron.updateTime || cron.createTime">
                          <div class="text-label-medium text-medium-emphasis mb-1">Updated</div>
                          <div class="text-body-medium">{{ formatTimestampLike(cron.updateTime ?? cron.createTime) }}</div>
                        </div>

                        <div v-if="cron.lastRunTime">
                          <div class="text-label-medium text-medium-emphasis mb-1">Last run</div>
                          <div class="text-body-medium">{{ formatTimestampLike(cron.lastRunTime) }}</div>
                        </div>

                        <div v-if="cron.archiveTime">
                          <div class="text-label-medium text-medium-emphasis mb-1">Archived</div>
                          <div class="text-body-medium">{{ formatTimestampLike(cron.archiveTime) }}</div>
                        </div>
                      </div>
                    </v-card-text>
                  </v-card>
                </div>
              </div>
            </div>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </v-container>

  <v-dialog
    v-model="createDialogOpen"
    max-width="640"
  >
    <v-card
      rounded="xl"
      class="create-dialog"
    >
      <v-card-title class="text-headline-small px-6 pt-6 pb-2"> Create schedule </v-card-title>

      <v-card-text class="px-6 pb-4">
        <v-form>
          <div class="text-body-medium text-medium-emphasis mb-5">Create either a recurring automation with a cron expression or a once off automation that runs at a specific date and time.</div>

          <v-btn-toggle
            v-model="createForm.type"
            mandatory
            class="mb-5"
            variant="outlined"
          >
            <v-btn :value="Cron_Type.CRON"> Recurring </v-btn>
            <v-btn :value="Cron_Type.AT"> Once off </v-btn>
          </v-btn-toggle>

          <v-textarea
            v-if="createForm.type === Cron_Type.CRON"
            v-model="createForm.initialPrompt"
            label="Initial prompt"
            hint="Optional setup prompt that runs first to establish recurring context."
            persistent-hint
            variant="outlined"
            rows="3"
            auto-grow
            class="mb-4"
          />

          <v-textarea
            v-model="createForm.prompt"
            :label="createForm.type === Cron_Type.CRON ? 'Recurring prompt' : 'Prompt'"
            :hint="createForm.type === Cron_Type.CRON ? 'Sent on each recurring run after the optional initial prompt.' : undefined"
            :persistent-hint="createForm.type === Cron_Type.CRON"
            variant="outlined"
            rows="4"
            auto-grow
            class="mb-4"
          />

          <div
            v-if="createForm.type === Cron_Type.CRON"
            class="recurring-builder mb-7"
          >
            <div class="text-label-medium text-medium-emphasis mb-2">Quick picks</div>
            <div class="recurring-builder__presets mb-4">
              <v-btn
                v-for="preset in recurringPresets"
                :key="preset.id"
                size="small"
                variant="outlined"
                class="text-none"
                @click="applyRecurringPreset(preset.id)"
              >
                {{ preset.label }}
              </v-btn>
            </div>

            <div class="recurring-builder__grid mb-4">
              <v-select
                v-model="createForm.recurringPattern"
                :items="recurringPatternOptions"
                item-title="title"
                item-value="value"
                label="Frequency"
                variant="outlined"
                hide-details
              />

              <v-text-field
                v-if="createForm.recurringPattern === 'hourly'"
                v-model="createForm.recurringMinute"
                label="Minute past"
                type="number"
                min="0"
                max="59"
                variant="outlined"
                hide-details
              />

              <v-text-field
                v-else
                v-model="createForm.recurringTime"
                label="Run at"
                type="time"
                variant="outlined"
                hide-details
              />

              <v-select
                v-if="createForm.recurringPattern === 'weekly'"
                v-model="createForm.recurringWeekday"
                :items="weekdayOptions"
                item-title="title"
                item-value="value"
                label="Day"
                variant="outlined"
                hide-details
              />

              <v-text-field
                v-if="createForm.recurringPattern === 'monthly'"
                v-model="createForm.recurringMonthDay"
                label="Day of month"
                type="number"
                min="1"
                max="31"
                variant="outlined"
                hide-details
              />
            </div>

            <v-text-field
              v-model="createForm.expr"
              label="Cron expression"
              :hint="createForm.manualCron ? 'Edit the five-part cron expression directly.' : recurringSummary"
              persistent-hint
              variant="outlined"
              :readonly="!createForm.manualCron"
            >
              <template #append-inner>
                <v-btn
                  size="small"
                  variant="text"
                  class="text-none recurring-builder__manual-btn"
                  @click.stop="toggleManualCron"
                >
                  {{ createForm.manualCron ? 'Use guided builder' : 'Edit manually' }}
                </v-btn>
              </template>
            </v-text-field>
          </div>

          <v-row
            v-else
            class="mb-5"
          >
            <v-col
              cols="12"
              md="7"
            >
              <v-date-input
                v-model="createForm.atDate"
                label="Run on"
                hint="Choose the date for this once off automation."
                persistent-hint
                variant="outlined"
                prepend-icon=""
                control-variant="modal"
                input-format="yyyy-MM-dd"
                :display-format="formatRunDateInput"
              />
            </v-col>

            <v-col
              cols="12"
              md="5"
            >
              <v-text-field
                v-model="createForm.atTime"
                label="Run at"
                type="time"
                hint="Choose the local time for this once off automation."
                persistent-hint
                variant="outlined"
              />
            </v-col>
          </v-row>

          <v-text-field
            v-model="createForm.timezone"
            label="Timezone"
            :hint="createForm.type === Cron_Type.CRON ? 'Example: Africa/Johannesburg' : 'Used with the selected date and time for this once off automation.'"
            persistent-hint
            variant="outlined"
          />
        </v-form>
      </v-card-text>

      <v-card-actions class="px-6 pb-6">
        <v-spacer />
        <v-btn
          variant="text"
          class="text-none"
          :disabled="creating"
          @click="closeCreateDialog"
        >
          Cancel
        </v-btn>
        <v-btn
          color="primary"
          variant="flat"
          rounded="pill"
          class="text-none"
          :loading="creating"
          @click="handleCreateCron"
        >
          Create schedule
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script setup lang="ts">
  import { useAppStore } from '@/store/app'
  import { useSnackbar } from '@/store/snackbar'
  import { computed, onMounted, reactive, ref, watch } from 'vue'
  import { useTheme } from 'vuetify'
  import {
    type Cron,
    type CronType,
    createCron,
    Cron_Type,
    deleteCron,
    formatSchedulerClientError,
    formatTimestampLike,
    listCrons,
    runCron,
    timestampLikeToMs,
  } from './client'

  /** Predefined recurring schedule patterns for the create/edit dialogs. */
  type RecurringPattern = 'hourly' | 'daily' | 'weekdays' | 'weekly' | 'monthly'

  /** Strict timestamp shape for once-off schedule requests before protojson encoding. */
  type TimestampInput = {
    seconds?: bigint
    nanos?: number
  }

  const snackbar = useSnackbar()
  const appStore = useAppStore()
  const theme = useTheme()

  const loading = ref(false)
  const creating = ref(false)
  const error = ref('')
  const crons = ref<Cron[]>([])
  const createDialogOpen = ref(false)
  const deletingCronNames = ref(new Set<string>())
  const runningCronNames = ref(new Set<string>())
  const recurringPresets = [
    { id: 'hourly', label: 'Every hour' },
    { id: 'daily', label: 'Every day at 09:00' },
    { id: 'weekdays', label: 'Weekdays at 09:00' },
    { id: 'weekly', label: 'Every Monday at 09:00' },
    { id: 'monthly', label: 'Monthly on the 1st' },
  ] as const
  const recurringPatternOptions = [
    { title: 'Every hour', value: 'hourly' },
    { title: 'Every day', value: 'daily' },
    { title: 'Weekdays', value: 'weekdays' },
    { title: 'Every week', value: 'weekly' },
    { title: 'Every month', value: 'monthly' },
  ] as const
  const weekdayOptions = [
    { title: 'Monday', value: '1' },
    { title: 'Tuesday', value: '2' },
    { title: 'Wednesday', value: '3' },
    { title: 'Thursday', value: '4' },
    { title: 'Friday', value: '5' },
    { title: 'Saturday', value: '6' },
    { title: 'Sunday', value: '0' },
  ] as const
  const createForm = reactive({
    type: Cron_Type.CRON as CronType,
    initialPrompt: '',
    prompt: '',
    expr: '',
    recurringPattern: 'daily' as RecurringPattern,
    recurringTime: '09:00',
    recurringMinute: '0',
    recurringWeekday: '1',
    recurringMonthDay: '1',
    manualCron: false,
    atDate: null as Date | null,
    atTime: '',
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC',
  })

  const cronCount = computed(() => crons.value.length)
  const activeCrons = computed(() => crons.value.filter((cron) => !isArchivedCron(cron)))
  const archivedCrons = computed(() => crons.value.filter((cron) => isArchivedCron(cron)))
  const isDarkTheme = computed(() => theme.global.current.value.dark)
  const recurringSummary = computed(() => {
    switch (createForm.recurringPattern) {
      case 'hourly':
        return `Runs every hour at minute ${normalizeMinute(createForm.recurringMinute)}.`
      case 'daily':
        return `Runs every day at ${normalizeTime(createForm.recurringTime)}.`
      case 'weekdays':
        return `Runs on weekdays at ${normalizeTime(createForm.recurringTime)}.`
      case 'weekly':
        return `Runs every ${weekdayLabel(createForm.recurringWeekday)} at ${normalizeTime(createForm.recurringTime)}.`
      case 'monthly':
        return `Runs on day ${normalizeMonthDay(createForm.recurringMonthDay)} of each month at ${normalizeTime(createForm.recurringTime)}.`
      default:
        return 'Generated from the guided builder.'
    }
  })

  function cronTypeLabel(cron: Cron): string {
    if (cron.type === Cron_Type.AT) return 'Once off'
    if (cron.type === Cron_Type.CRON) return 'Recurring'
    return 'Scheduled'
  }

  function isArchivedCron(cron: Cron): boolean {
    return Boolean(cron.archiveTime)
  }

  function cronScheduleCopy(cron: Cron): string {
    if (cron.type === Cron_Type.AT) {
      return cron.at ? formatTimestampLike(cron.at) : 'Runs once'
    }

    const parts = [cron.expr, cron.timezone].filter(Boolean)
    return parts.join(' • ') || 'Recurring schedule'
  }

  function formatRunDateInput(value: unknown): string {
    const date = value instanceof Date ? value : new Date(String(value))
    if (Number.isNaN(date.getTime())) return ''

    const year = date.getFullYear()
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    return `${year}-${month}-${day}`
  }

  function normalizeMinute(value: string): string {
    const minute = Number(value)
    if (!Number.isInteger(minute)) return '0'
    return String(Math.min(59, Math.max(0, minute)))
  }

  function normalizeMonthDay(value: string): string {
    const day = Number(value)
    if (!Number.isInteger(day)) return '1'
    return String(Math.min(31, Math.max(1, day)))
  }

  function normalizeTime(value: string): string {
    if (/^\d{2}:\d{2}$/.test(value)) return value
    return '09:00'
  }

  function weekdayLabel(value: string): string {
    return weekdayOptions.find((item) => item.value === value)?.title ?? 'Monday'
  }

  function buildRecurringExpr(): string {
    switch (createForm.recurringPattern) {
      case 'hourly':
        return `${normalizeMinute(createForm.recurringMinute)} * * * *`
      case 'daily': {
        const [hour, minute] = normalizeTime(createForm.recurringTime).split(':')
        return `${minute} ${hour} * * *`
      }
      case 'weekdays': {
        const [hour, minute] = normalizeTime(createForm.recurringTime).split(':')
        return `${minute} ${hour} * * 1-5`
      }
      case 'weekly': {
        const [hour, minute] = normalizeTime(createForm.recurringTime).split(':')
        return `${minute} ${hour} * * ${createForm.recurringWeekday}`
      }
      case 'monthly': {
        const [hour, minute] = normalizeTime(createForm.recurringTime).split(':')
        return `${minute} ${hour} ${normalizeMonthDay(createForm.recurringMonthDay)} * *`
      }
      default:
        return ''
    }
  }

  function applyRecurringPreset(presetId: string): void {
    createForm.manualCron = false

    switch (presetId) {
      case 'hourly':
        createForm.recurringPattern = 'hourly'
        createForm.recurringMinute = '0'
        break
      case 'daily':
        createForm.recurringPattern = 'daily'
        createForm.recurringTime = '09:00'
        break
      case 'weekdays':
        createForm.recurringPattern = 'weekdays'
        createForm.recurringTime = '09:00'
        break
      case 'weekly':
        createForm.recurringPattern = 'weekly'
        createForm.recurringTime = '09:00'
        createForm.recurringWeekday = '1'
        break
      case 'monthly':
        createForm.recurringPattern = 'monthly'
        createForm.recurringTime = '09:00'
        createForm.recurringMonthDay = '1'
        break
    }

    createForm.expr = buildRecurringExpr()
  }

  function toggleManualCron(): void {
    createForm.manualCron = !createForm.manualCron
    if (!createForm.manualCron) {
      createForm.expr = buildRecurringExpr()
    }
  }

  async function loadCrons(): Promise<void> {
    loading.value = true
    error.value = ''

    try {
      const response = await listCrons({
        pageSize: 100,
      })
      crons.value = [...(response.crons ?? [])].sort(
        (left, right) => timestampLikeToMs(right.updateTime ?? right.createTime) - timestampLikeToMs(left.updateTime ?? left.createTime),
      )
    } catch (err) {
      console.error('[automation] Failed to load crons', err)
      error.value = formatSchedulerClientError(err, 'Failed to load schedules.')
      crons.value = []
    } finally {
      loading.value = false
    }
  }

  function closeCreateDialog(): void {
    createDialogOpen.value = false
    createForm.type = Cron_Type.CRON
    createForm.initialPrompt = ''
    createForm.prompt = ''
    createForm.expr = '0 9 * * *'
    createForm.recurringPattern = 'daily'
    createForm.recurringTime = '09:00'
    createForm.recurringMinute = '0'
    createForm.recurringWeekday = '1'
    createForm.recurringMonthDay = '1'
    createForm.manualCron = false
    createForm.atDate = null
    createForm.atTime = ''
    createForm.timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  }

  function toTimestamp(dateValue: Date | null, timeText: string): TimestampInput | undefined {
    const normalizedTime = timeText.trim()
    if (!dateValue || !normalizedTime) return undefined

    const date = new Date(dateValue)
    const [hoursText, minutesText] = normalizedTime.split(':')
    const hours = Number(hoursText)
    const minutes = Number(minutesText)
    if (!Number.isInteger(hours) || !Number.isInteger(minutes)) return undefined
    date.setHours(hours, minutes, 0, 0)
    if (Number.isNaN(date.getTime())) return undefined

    const millis = date.getTime()
    return {
      seconds: BigInt(Math.floor(millis / 1000)),
      nanos: (millis % 1000) * 1_000_000,
    }
  }

  async function handleCreateCron(): Promise<void> {
    const type = createForm.type
    const initialPrompt = createForm.initialPrompt.trim()
    const prompt = createForm.prompt.trim()
    const expr = createForm.expr.trim()
    const atDate = createForm.atDate
    const atTime = createForm.atTime.trim()
    const timezone = createForm.timezone.trim()
    const owner = appStore.userDisplayName ?? ''
    const email = appStore.userEmail ?? ''

    if (!prompt) {
      snackbar.error('Enter a prompt for the schedule.')
      return
    }

    if (!timezone) {
      snackbar.error('Enter a timezone.')
      return
    }

    if (type === Cron_Type.CRON) {
      if (!expr) {
        snackbar.error('Enter a cron expression.')
        return
      }
    } else {
      if (!atDate || !atTime) {
        snackbar.error('Choose when the once-off schedule should run.')
        return
      }
    }

    if (!owner || !email) {
      snackbar.error('Your user details are not loaded yet. Refresh and try again.')
      return
    }

    creating.value = true

    try {
      const atTimestamp = type === Cron_Type.AT ? toTimestamp(atDate, atTime) : undefined

      if (type === Cron_Type.AT && !atTimestamp) {
        snackbar.error('Enter a valid date and time for the once-off schedule.')
        return
      }

      const cron = await createCron({
        cron: {
          prompt,
          initialPrompt: type === Cron_Type.CRON ? initialPrompt : '',
          expr: type === Cron_Type.CRON ? expr : '',
          timezone,
          at: atTimestamp,
          type,
          owner,
          email,
        },
      })

      crons.value = [cron, ...crons.value.filter((item) => item.name !== cron.name)]
      snackbar.success(type === Cron_Type.AT ? 'Once off schedule created.' : 'Recurring schedule created.')
      closeCreateDialog()
    } catch (err) {
      console.error('[automation] Failed to create cron', err)
      snackbar.error(formatSchedulerClientError(err, 'Failed to create schedule.'))
    } finally {
      creating.value = false
    }
  }

  async function handleDeleteCron(cron: Cron): Promise<void> {
    deletingCronNames.value = new Set(deletingCronNames.value).add(cron.name)

    try {
      await deleteCron({ name: cron.name })
      crons.value = crons.value.filter((item) => item.name !== cron.name)
      snackbar.success('Schedule deleted.')
    } catch (err) {
      console.error('[automation] Failed to delete cron', err)
      snackbar.error(formatSchedulerClientError(err, 'Failed to delete schedule.'))
    } finally {
      const next = new Set(deletingCronNames.value)
      next.delete(cron.name)
      deletingCronNames.value = next
    }
  }

  async function handleRunCron(cron: Cron): Promise<void> {
    const id = cron.name.split('/').pop() ?? ''
    if (!id) {
      snackbar.error('This schedule is missing an ID and cannot be run.')
      return
    }

    runningCronNames.value = new Set(runningCronNames.value).add(cron.name)

    try {
      await runCron({ id })
      snackbar.success('Schedule triggered.')
    } catch (err) {
      console.error('[automation] Failed to run cron', err)
      snackbar.error(formatSchedulerClientError(err, 'Failed to run schedule.'))
    } finally {
      const next = new Set(runningCronNames.value)
      next.delete(cron.name)
      runningCronNames.value = next
    }
  }

  onMounted(async () => {
    createForm.expr = buildRecurringExpr()
    await loadCrons()
  })

  watch(
    () => [createForm.type, createForm.recurringPattern, createForm.recurringTime, createForm.recurringMinute, createForm.recurringWeekday, createForm.recurringMonthDay, createForm.manualCron],
    () => {
      if (createForm.type !== Cron_Type.CRON || createForm.manualCron) return
      createForm.expr = buildRecurringExpr()
    },
  )
</script>

<style scoped lang="scss">
  .automation-page {
    height: 100%;
    min-height: 0;
    align-items: flex-start;
    overflow-y: auto;
    overflow-x: hidden;
    -webkit-overflow-scrolling: touch;
  }

  .automation-title {
    line-height: 1.05;
    letter-spacing: -0.04em !important;
  }

  .summary-card {
    background: rgba(var(--v-theme-surfaceContainer), 0.72);
    border: 1px solid rgba(var(--v-theme-on-surface), 0.06);
    box-shadow: 0 18px 42px rgba(0, 0, 0, 0.1);
  }

  .automation-panel {
    background: rgb(var(--v-theme-surface));
    border: 1px solid rgba(var(--v-theme-on-surface), 0.08);
  }

  .actions-toolbar {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    flex-wrap: wrap;
    gap: 1rem;
  }

  .loading-grid,
  .cron-grid {
    display: grid;
    gap: 1rem;
    grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  }

  .cron-sections {
    display: grid;
    gap: 2.5rem;
  }

  .cron-section--archived {
    opacity: 0.78;
  }

  .loading-card {
    border-radius: 1.5rem;
  }

  .cron-card {
    background: rgba(var(--v-theme-surfaceContainer), 0.64);
    border: 1px solid rgba(var(--v-theme-on-surface), 0.06);
    box-shadow: 0 18px 42px rgba(0, 0, 0, 0.1);
  }

  .cron-grid--archived {
    gap: 0.875rem;
  }

  .cron-card--archived {
    background: rgba(var(--v-theme-surfaceContainer), 0.34);
    border-color: rgba(var(--v-theme-on-surface), 0.05);
    box-shadow: none;
  }

  .cron-card__header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 1rem;
    margin-bottom: 1rem;
  }

  .cron-card__title-wrap {
    min-width: 0;
  }

  .cron-card__chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }

  .cron-card__actions {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    flex: 0 0 auto;
  }

  .cron-card__actions--archived {
    opacity: 0.72;
  }

  .cron-card__prompt {
    margin-bottom: 1.25rem;
    padding: 0.9rem 1rem;
    border-radius: 1rem;
    background: rgba(var(--v-theme-on-surface), 0.04);
    border: 1px solid rgba(var(--v-theme-on-surface), 0.06);
  }

  .cron-card__prompt .text-body-medium {
    white-space: pre-wrap;
  }

  .cron-card__initial-prompt {
    margin-bottom: 1rem;
    padding: 0.9rem 1rem;
    border-radius: 1rem;
    background: rgba(var(--v-theme-on-surface), 0.04);
    border: 1px solid rgba(var(--v-theme-on-surface), 0.06);
    white-space: pre-wrap;
  }

  .cron-card__meta {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 1rem;
  }

  .empty-state {
    background: rgba(var(--v-theme-surfaceContainer), 0.5);
    border: 1px solid rgba(var(--v-theme-on-surface), 0.06);
  }

  .create-dialog {
    background: rgb(var(--v-theme-surface));
  }

  .recurring-builder__presets {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }

  .recurring-builder__grid {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 1rem;
    align-items: start;
  }

  .recurring-builder__manual-btn {
    margin-inline-end: -0.25rem;
  }

  .empty-state__icon-wrap {
    width: 3.5rem;
    height: 3.5rem;
    border-radius: 999px;
    display: grid;
    place-items: center;
    flex: 0 0 auto;
    background: rgba(var(--v-theme-on-surface), 0.05);
    color: rgba(var(--v-theme-on-surface), 0.72);
  }

  .empty-state__content {
    flex: 1;
    min-width: 0;
  }

  .empty-state__actions {
    flex: 0 0 auto;
  }

  .flex-1-1 {
    flex: 1 1 auto;
    min-width: 0;
  }

  .automation-page--dark .summary-card,
  .automation-page--dark .cron-card,
  .automation-page--dark .empty-state,
  .automation-page--dark .automation-panel {
    background: rgba(var(--v-theme-surfaceContainer), 0.44);
    border-color: rgba(var(--v-theme-on-surface), 0.08);
    box-shadow: none;
  }

  .automation-page--dark .cron-card--archived {
    background: rgba(var(--v-theme-surfaceContainer), 0.24);
    border-color: rgba(var(--v-theme-on-surface), 0.06);
  }

  @media (max-width: 900px) {
    .cron-card__header,
    .actions-toolbar {
      flex-direction: column;
      align-items: flex-start;
    }

    .cron-card__actions {
      width: 100%;
      justify-content: flex-start;
      flex-wrap: wrap;
    }

    .cron-card__meta {
      grid-template-columns: 1fr;
    }

    .recurring-builder__grid,
    .run-at-grid {
      grid-template-columns: 1fr;
    }

    .empty-state__actions {
      width: 100%;
    }
  }
</style>
