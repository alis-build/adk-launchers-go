<script setup lang="ts">
  import { useAppStore } from '@/store/app'

  const appStore = useAppStore()
  const emit = defineEmits<{
    /** Parent should close the profile menu overlay. */
    close: []
  }>()

  /** Signs the user out by navigating to the auth signout endpoint. */
  const signOut = () => {
    emit('close')
    window.location.href = '/auth/logout'
  }
</script>

<style scoped lang="scss">
  .profile-menu-card {
    border-radius: 28px;
    background: rgb(var(--v-theme-surface));
    color: rgb(var(--v-theme-on-surface));
    border: 1px solid rgba(var(--v-theme-on-surface), 0.08);
  }

  .camera-icon-wrapper {
    position: absolute;
    bottom: 0;
    right: 0;
    background: rgba(var(--v-theme-on-surface), 0.72);
    border-radius: 50%;
    width: 24px;
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 2px solid rgb(var(--v-theme-surface));
  }

  .sign-out-card {
    border-radius: 24px;
    transition: background-color 0.2s;
    background: rgba(var(--v-theme-on-surface), 0.04);
    color: rgb(var(--v-theme-on-surface));

    &:hover {
      background: rgba(var(--v-theme-on-surface), 0.08);
    }
  }

  .profile-menu-email {
    color: rgba(var(--v-theme-on-surface), 0.72);
  }

  .profile-menu-action-icon {
    color: rgba(var(--v-theme-on-surface), 0.64);
  }
</style>

<template>
  <v-card
    width="400"
    class="profile-menu-card pt-6 pb-2 text-center"
    elevation="4"
    rounded="xl"
  >
    <div class="mb-5">
      <div class="profile-menu-email text-body-medium font-weight-medium mb-4">
        {{ appStore.userEmail }}
      </div>

      <div class="position-relative d-inline-block mb-2">
        <v-avatar
          size="80"
          color="primary"
        >
          <v-img
            v-if="appStore.userProfilePicture"
            :src="appStore.userProfilePicture"
            cover
          />
          <span
            v-else
            class="text-headline-large"
          >
            {{ appStore.userInitials }}
          </span>
        </v-avatar>
        <div class="camera-icon-wrapper">
          <v-icon
            icon="photo_camera"
            size="14"
            color="white"
          />
        </div>
      </div>

      <h2 class="text-headline-medium font-weight-regular mt-2 mb-0">Hi, {{ appStore.userDisplayName.split(' ')[0] }}!</h2>
    </div>

    <div class="px-4 mb-2">
      <v-card
        flat
        rounded="lg"
        class="d-flex align-center cursor-pointer sign-out-card py-3 px-4"
        @click="signOut"
      >
        <v-icon
          icon="logout"
          class="profile-menu-action-icon mr-3"
        />
        <span>Sign out</span>
      </v-card>
    </div>
  </v-card>
</template>
