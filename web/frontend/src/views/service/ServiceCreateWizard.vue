<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { listServiceTypes } from '@/api/config'
import { getCustomers } from '@/api/customers'
import { createService } from '@/api/services'
import SchemaFields from '@/components/SchemaFields.vue'

const router = useRouter()
const toast = useToast()

const activeStep = ref('1')

const stepperItems = [
  { value: '1', title: 'Product' },
  { value: '2', title: 'Details' },
]

const capacityTypes = ref([])
const typesError = ref('')
const typesLoading = ref(true)

const productOptions = computed(() => [
  ...capacityTypes.value.map((t) => ({
    label: t.description ? `${t.name} — ${t.description}` : t.name,
    value: t.name,
  })),
  { label: 'Wavelength', value: 'WAVELENGTH' },
  { label: 'Fiber', value: 'FIBER' },
])

// API service types are capacity products: they carry a ServiceType and a
// CN/CI prefix. Wavelength and Fiber have no service type.

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
const isCapacityProduct = computed(() => !['WAVELENGTH', 'FIBER'].includes(product.value))
const serviceType = computed(() => (isCapacityProduct.value ? product.value : ''))
const selectedType = computed(() => capacityTypes.value.find((t) => t.name === product.value))
const schemaFields = computed(() => selectedType.value?.schema ?? [])
const schemaValues = ref({})

// A category chosen for one product isn't necessarily valid for another
// (e.g. picking Fiber's "LI" then going back and switching to ELAN), so
// clear it whenever the available options change.
watch(categoryOptions, () => {
  category.value = null
})

watch(product, () => {
  schemaValues.value = {}
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

onMounted(() => {
  getCustomers()
    .then((data) => {
      customers.value = data ?? []
    })
    .catch(() => {
      // Customer names are only needed for the optional company select, not critical.
    })
  typesLoading.value = true
  listServiceTypes()
    .then((rows) => {
      capacityTypes.value = rows ?? []
      if (!capacityTypes.value.length) {
        typesError.value = 'No service types defined. Add them under Config → Service types.'
      }
    })
    .catch(() => {
      typesError.value = 'Failed to load service types.'
    })
    .finally(() => {
      typesLoading.value = false
    })
})

function schemaMissingRequired() {
  return schemaFields.value.some((f) => {
    if (!f.required) return false
    const v = schemaValues.value[f.name]
    return v === null || v === undefined || v === ''
  })
}

function handleProductNext() {
  submittedStep1.value = true
  if (!product.value || !category.value) return
  activeStep.value = '2'
}

function handleCreate() {
  submittedStep2.value = true
  if (isCapacityProduct.value && schemaMissingRequired()) return

  completing.value = true
  const serviceID = form.value.serviceID
  const fields = { ...schemaValues.value }
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
    service_type: serviceType.value,
    bandwidth_mbps: Number(fields.bandwidth_mbps) || 0,
    fields,
    max_mac_addresses: Number(fields.max_mac_addresses) || 0,
  }

  createService(payload)
    .then((created) => {
      logLines.value.push(`Created service ${created.service_id}`)
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

    <UStepper v-model="activeStep" :items="stepperItems" linear class="mb-6" />

    <div v-if="activeStep === '1'" class="flex flex-col gap-6 py-4">
      <div>
        <label class="block font-bold mb-3">Product</label>
        <p v-if="typesLoading" class="text-muted-color text-sm mb-2">Loading service types…</p>
        <p v-else-if="typesError" class="text-red-500 text-sm mb-2">{{ typesError }}</p>
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
      </div>
      <div>
        <label class="block font-bold mb-3">Category</label>
        <URadioGroup
          v-model="category"
          :items="categoryOptions"
          :color="submittedStep1 && !category ? 'error' : undefined"
          :highlight="submittedStep1 && !category"
        />
        <small v-if="submittedStep1 && !category" class="text-red-500 block mt-2">
          Select a category.
        </small>
      </div>
      <div>
        <label class="block font-bold mb-3">Service ID</label>
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
      </div>
      <div class="flex justify-between mt-6">
        <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="cancelWizard" />
        <UButton label="Next" icon="i-lucide-arrow-right" trailing @click="handleProductNext" />
      </div>
    </div>

    <div v-else-if="activeStep === '2'" class="flex flex-col gap-6 py-4">
      <div>
        <label class="block font-bold mb-3">Company</label>
        <USelectMenu
          v-model="form.company"
          :items="customerOptions"
          value-key="id"
          label-key="name"
          placeholder="Select a customer (optional)"
          class="w-full"
        />
      </div>
      <div>
        <label class="block font-bold mb-3">Deliverypoint A</label>
        <UInput v-model="form.deliverypoint1" class="w-full" />
      </div>
      <div>
        <label class="block font-bold mb-3">Deliverypoint B</label>
        <UInput v-model="form.deliverypoint2" class="w-full" />
      </div>
      <div>
        <label class="block font-bold mb-3">Product</label>
        <UInput v-model="form.product" class="w-full" />
      </div>
      <div>
        <label class="block font-bold mb-3">Service</label>
        <UInput v-model="form.service" class="w-full" />
      </div>
      <SchemaFields
        v-if="isCapacityProduct && schemaFields.length"
        v-model="schemaValues"
        :fields="schemaFields"
        :submitted="submittedStep2"
      />
      <div>
        <label class="block font-bold mb-3">Comment</label>
        <UTextarea v-model="form.comment" :rows="3" class="w-full" />
      </div>
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
