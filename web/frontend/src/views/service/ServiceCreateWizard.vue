<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { getCustomers } from '@/api/customers'
import { createService } from '@/api/services'
import { usePageForm } from '@/composables/useFormGuard'

const router = useRouter()
const toast = useToast()

const activeStep = ref('1')

const stepperItems = [
  { value: '1', title: 'Product' },
  { value: '2', title: 'Details' },
]

const productOptions = [
  { label: 'Capacity (CN/CI)', value: 'CAPACITY' },
  { label: 'Wavelength', value: 'WAVELENGTH' },
  { label: 'Fiber', value: 'FIBER' },
]

const categoryOptionsByProduct = {
  FIBER: [
    { label: 'LF — External customer', value: 'LF' },
    { label: 'LI — Internal use', value: 'LI' },
  ],
  WAVELENGTH: [
    { label: 'VL — External customer', value: 'VL' },
    { label: 'VI — Internal use', value: 'VI' },
  ],
}
const DEFAULT_CATEGORY_OPTIONS = [
  { label: 'CN — External customer', value: 'CN' },
  { label: 'CI — Internal use', value: 'CI' },
]

const product = ref(null)
const category = ref(null)

const categoryOptions = computed(
  () => categoryOptionsByProduct[product.value] ?? DEFAULT_CATEGORY_OPTIONS,
)

watch(categoryOptions, () => {
  category.value = null
})

const completing = ref(false)
const submittedStep1 = ref(false)
const submittedStep2 = ref(false)

const customers = ref([])
const customerOptions = computed(() => customers.value.map((c) => ({ id: c.id, name: c.name })))

const logLines = ref([])

const form = ref({
  company: null,
  serviceID: null,
  deliverypoint1: '',
  deliverypoint2: '',
  product: '',
  service: '',
  comment: '',
})

const pageForm = computed(() => ({
  product: product.value,
  category: category.value,
  step: activeStep.value,
  ...form.value,
}))
const { mark } = usePageForm(pageForm)

onMounted(() => {
  mark()
  getCustomers()
    .then((data) => {
      customers.value = data ?? []
    })
    .catch(() => {})
})

function handleProductNext() {
  submittedStep1.value = true
  if (!product.value || !category.value) return
  activeStep.value = '2'
}

function handleCreate() {
  submittedStep2.value = true
  completing.value = true
  const serviceID = form.value.serviceID
  const payload = {
    category: category.value,
    service_id:
      serviceID === null || serviceID === undefined
        ? ''
        : `${category.value}${String(serviceID).padStart(5, '0')}`,
    company: form.value.company,
    deliverypoint1: form.value.deliverypoint1,
    deliverypoint2: form.value.deliverypoint2,
    product: form.value.product,
    service: form.value.service,
    comment: form.value.comment,
  }

  createService(payload)
    .then((created) => {
      logLines.value.push(`Created service ${created.service_id}`)
      mark()
      toast.add({
        color: 'success',
        title: 'Successful',
        description: `Service ${created.service_id} created`,
        duration: 3000,
      })
      setTimeout(() => router.push('/service'), 1500)
    })
    .catch(() => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: 'Failed to create service.',
        duration: 3000,
      })
    })
    .finally(() => {
      completing.value = false
    })
}

function cancelWizard() {
  router.push('/service')
}
</script>

<template>
  <div class="card">
    <h4 class="mt-0">New service</h4>
    <p class="text-muted-color text-sm">
      Commercial inventory. Realize a capacity service from a definition on the Config page.
    </p>

    <UStepper v-model="activeStep" :items="stepperItems" linear class="mb-6" />

    <div v-if="activeStep === '1'" class="flex flex-col gap-6 py-4">
      <UFormField label="Product">
        <URadioGroup
          v-model="product"
          :items="productOptions"
          variant="card"
          :color="submittedStep1 && !product ? 'error' : undefined"
          :highlight="submittedStep1 && !product"
        />
        <small v-if="submittedStep1 && !product" class="text-red-500 block mt-2">
          Select a product.
        </small>
      </UFormField>
      <UFormField label="Category">
        <URadioGroup
          v-model="category"
          :items="categoryOptions"
          :color="submittedStep1 && !category ? 'error' : undefined"
          :highlight="submittedStep1 && !category"
        />
        <small v-if="submittedStep1 && !category" class="text-red-500 block mt-2">
          Select a category.
        </small>
      </UFormField>
      <UFormField label="Service ID">
        <UInput
          v-model="form.serviceID"
          type="number"
          placeholder="Leave blank to auto-assign"
          :min="0"
          :max="99999"
          class="w-full"
        >
          <template #leading>
            <span class="text-muted text-sm">{{ category }}</span>
          </template>
        </UInput>
      </UFormField>
      <div class="flex justify-between mt-6">
        <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="cancelWizard" />
        <UButton label="Next" icon="i-lucide-arrow-right" trailing @click="handleProductNext" />
      </div>
    </div>

    <div v-else-if="activeStep === '2'" class="flex flex-col gap-6 py-4">
      <UFormField label="Company">
        <USelectMenu
          v-model="form.company"
          :items="customerOptions"
          value-key="id"
          label-key="name"
          placeholder="Select a customer (optional)"
          class="w-full"
        />
      </UFormField>
      <UFormField label="Deliverypoint A">
        <UInput v-model="form.deliverypoint1" class="w-full" />
      </UFormField>
      <UFormField label="Deliverypoint B">
        <UInput v-model="form.deliverypoint2" class="w-full" />
      </UFormField>
      <UFormField label="Product">
        <UInput v-model="form.product" class="w-full" />
      </UFormField>
      <UFormField label="Service">
        <UInput v-model="form.service" class="w-full" />
      </UFormField>
      <UFormField label="Comment">
        <UTextarea v-model="form.comment" :rows="3" class="w-full" />
      </UFormField>
      <div class="flex justify-between mt-6">
        <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="cancelWizard" />
        <div class="flex gap-2">
          <UButton label="Back" color="neutral" variant="outline" @click="activeStep = '1'" />
          <UButton
            label="Create service"
            icon="i-lucide-check"
            :loading="completing"
            @click="handleCreate"
          />
        </div>
      </div>
    </div>
  </div>

  <div v-if="logLines.length" class="card mt-4">
    <h5 class="mt-0">Log</h5>
    <div v-for="(line, index) in logLines" :key="index">{{ line }}</div>
  </div>
</template>
