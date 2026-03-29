const tokenStorageKey = "ncs-admin-token";
const numberFormatter = new Intl.NumberFormat("th-TH", { maximumFractionDigits: 0 });
const state = {
  token: localStorage.getItem(tokenStorageKey) || "",
  config: null,
};

const elements = {
  tokenInput: document.getElementById("adminToken"),
  tokenStatus: document.getElementById("tokenStatus"),
  saveTokenBtn: document.getElementById("saveTokenBtn"),
  refreshBtn: document.getElementById("refreshConfigBtn"),
  formatBtn: document.getElementById("formatConfigBtn"),
  saveFullBtn: document.getElementById("saveFullConfigBtn"),
  configEditor: document.getElementById("configEditor"),
  toast: document.getElementById("toast"),
  servicesList: document.getElementById("servicesList"),
  customersList: document.getElementById("customersList"),
  priceTableBody: document.getElementById("priceTableBody"),
  promoTableBody: document.getElementById("promoTableBody"),
  addNewPriceBtn: document.getElementById("addNewPriceBtn"),
  addNewPromoBtn: document.getElementById("addNewPromoBtn"),
  priceModal: document.getElementById("priceModal"),
  promoModal: document.getElementById("promoModal"),
  priceModalForm: document.getElementById("priceModalForm"),
  promoModalForm: document.getElementById("promoModalForm"),
};

elements.tokenInput.value = state.token;

// Update token status badge
const updateTokenStatus = () => {
  if (state.token) {
    elements.tokenStatus.textContent = "✓ มีรหัสแล้ว";
    elements.tokenStatus.style.background = "#d4edda";
    elements.tokenStatus.style.color = "#155724";
  } else {
    elements.tokenStatus.textContent = "ยังไม่ได้ใส่รหัส";
    elements.tokenStatus.style.background = "#f8d7da";
    elements.tokenStatus.style.color = "#721c24";
  }
};

updateTokenStatus();

elements.saveTokenBtn.addEventListener("click", () => {
  state.token = elements.tokenInput.value.trim();
  if (!state.token) {
    showToast("ลบรหัสแล้ว กรุณาใส่รหัสก่อนใช้งาน", "error");
    localStorage.removeItem(tokenStorageKey);
    updateTokenStatus();
    return;
  }
  localStorage.setItem(tokenStorageKey, state.token);
  updateTokenStatus();
  showToast("บันทึกรหัสแล้ว ✓");
});

const requireToken = () => {
  if (!state.token) {
    showToast("กรุณาใส่รหัสผ่านแอดมินก่อน 🔐", "error");
    throw new Error("missing token");
  }
};

const adminFetch = async (path, options = {}) => {
  requireToken();
  const headers = new Headers(options.headers || {});
  headers.set("X-Admin-Token", state.token);
  if (options.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(path, {
    ...options,
    headers,
  });
  if (!response.ok) {
    let errorMsg = `Request failed (${response.status})`;
    try {
      const payload = await response.json();
      if (payload?.error) errorMsg = payload.error;
    } catch (_) {
      // ignore parse issues
    }
    throw new Error(errorMsg);
  }
  if (response.status === 204) return null;
  return response.json();
};

const refreshConfig = async () => {
  try {
    const data = await adminFetch("/admin/config/pricing");
    setConfig(data);
    showToast("รีเฟรชข้อมูลสำเร็จแล้ว ✅");
  } catch (err) {
    showToast(err.message, "error");
  }
};

elements.refreshBtn.addEventListener("click", refreshConfig);

const formatConfig = () => {
  try {
    const parsed = JSON.parse(elements.configEditor.value);
    elements.configEditor.value = JSON.stringify(parsed, null, 2);
    showToast("จัดรูปแบบเรียบร้อยแล้ว");
  } catch (_) {
    showToast("JSON ไม่ถูกต้อง กรุณาแก้ไขก่อน", "error");
  }
};

elements.formatBtn.addEventListener("click", formatConfig);

const saveFullConfig = async () => {
  try {
    const parsed = JSON.parse(elements.configEditor.value);
    await adminFetch("/admin/config/pricing", {
      method: "PUT",
      body: JSON.stringify(parsed),
    });
    showToast("บันทึกข้อมูลสำเร็จแล้ว ✅");
    await refreshConfig();
  } catch (err) {
    showToast(err.message || "ไม่สามารถบันทึกได้", "error");
  }
};

elements.saveFullBtn.addEventListener("click", saveFullConfig);

const num = (value) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

function setConfig(config) {
  state.config = config;
  if (elements.configEditor) {
    elements.configEditor.value = JSON.stringify(config, null, 2);
  }
  renderOverview();
  renderPriceTable();
  renderPromoTable();
}

function renderOverview() {
  renderServicePills();
  renderCustomerPills();
}

function renderPriceTable() {
  
  if (!state.config || !elements.priceTableBody) return;
  
  const rows = [];
  const items = state.config.items || {};
  
  // วนลูปทุกสินค้า -> ทุกขนาด -> ทุกบริการ -> ทุกลูกค้า -> ทุกแพคเกจ
  for (const [itemKey, itemData] of Object.entries(items)) {
    const itemName = itemData.name || itemKey;
    const sizes = itemData.sizes || {};
    
    for (const [sizeKey, sizeData] of Object.entries(sizes)) {
      const pricing = sizeData.pricing || {};
      
      for (const [serviceKey, servicePricing] of Object.entries(pricing)) {
        for (const [customerKey, packages] of Object.entries(servicePricing)) {
          for (const [packageKey, priceData] of Object.entries(packages)) {
            rows.push({
              itemKey,
              itemName,
              sizeKey,
              serviceKey,
              serviceName: getServiceName(serviceKey),
              customerKey,
              packageKey,
              fullPrice: priceData.full_price || 0,
              discount35: priceData.discount_35 || 0,
              discount50: priceData.discount_50 || 0,
            });
          }
        }
      }
    }
  }
  
  
  if (rows.length === 0) {
    elements.priceTableBody.innerHTML = '<tr><td colspan="9" class="empty-state">ยังไม่มีข้อมูลราคา กดปุ่ม "➕ เพิ่มราคาใหม่" เพื่อเริ่มต้น</td></tr>';
    return;
  }
  
  elements.priceTableBody.innerHTML = rows.map(row => `
    <tr>
      <td>${row.itemName}</td>
      <td>${row.sizeKey}</td>
      <td>${row.serviceName}</td>
      <td>${row.customerKey}</td>
      <td>${row.packageKey}</td>
      <td class="editable" onclick="editPrice('${row.itemKey}','${row.sizeKey}','${row.serviceKey}','${row.customerKey}','${row.packageKey}')">${formatMoney(row.fullPrice)}</td>
      <td class="editable" onclick="editPrice('${row.itemKey}','${row.sizeKey}','${row.serviceKey}','${row.customerKey}','${row.packageKey}')">${formatMoney(row.discount35)}</td>
      <td class="editable" onclick="editPrice('${row.itemKey}','${row.sizeKey}','${row.serviceKey}','${row.customerKey}','${row.packageKey}')">${formatMoney(row.discount50)}</td>
      <td class="actions-cell">
        <button class="btn-sm" onclick="editPrice('${row.itemKey}','${row.sizeKey}','${row.serviceKey}','${row.customerKey}','${row.packageKey}')">✏️ แก้ไข</button>
        <button class="btn-sm btn-delete" onclick="deletePrice('${row.itemKey}','${row.sizeKey}','${row.serviceKey}','${row.customerKey}','${row.packageKey}')">🗑️ ลบ</button>
      </td>
    </tr>
  `).join('');
}

function renderPromoTable() {
  if (!state.config || !elements.promoTableBody) return;
  
  const rows = [];
  const packages = state.config.packages || {};
  
  // Loop: packages (coupon/contract) -> services (disinfection/washing) -> quantities (2,3,5,10...)
  for (const [packageKey, packageData] of Object.entries(packages)) {
    const packageName = packageData.name || packageKey;
    
    // Skip 'name' and 'aliases' properties, loop only service keys
    for (const [serviceKey, quantities] of Object.entries(packageData)) {
      if (serviceKey === 'name' || serviceKey === 'aliases') continue;
      
      for (const [quantity, priceData] of Object.entries(quantities)) {
        rows.push({
          packageKey,
          packageName,
          serviceKey,
          serviceName: getServiceName(serviceKey),
          quantity,
          fullPrice: priceData.full_price || 0,
          discount: priceData.discount || 0,
          salePrice: priceData.sale_price || 0,
          perItem: priceData.per_item || 0,
          depositMin: priceData.deposit_min || 0,
        });
      }
    }
  }
  
  if (rows.length === 0) {
    elements.promoTableBody.innerHTML = '<tr><td colspan="9" class="empty-state">ยังไม่มีโปรโมชั่น กดปุ่ม "➕ เพิ่มโปรโมชั่นใหม่" เพื่อเริ่มต้น</td></tr>';
    return;
  }
  
  elements.promoTableBody.innerHTML = rows.map(row => `
    <tr>
      <td>${row.packageName}</td>
      <td>${row.serviceName}</td>
      <td>${row.quantity}</td>
      <td class="editable" onclick="editPromo('${row.packageKey}','${row.serviceKey}',${row.quantity})">${formatMoney(row.fullPrice)}</td>
      <td class="editable" onclick="editPromo('${row.packageKey}','${row.serviceKey}',${row.quantity})">${formatMoney(row.discount)}</td>
      <td class="editable" onclick="editPromo('${row.packageKey}','${row.serviceKey}',${row.quantity})">${formatMoney(row.salePrice)}</td>
      <td>${formatMoney(row.perItem)}</td>
      <td>${formatMoney(row.depositMin)}</td>
      <td class="actions-cell">
        <button class="btn-sm" onclick="editPromo('${row.packageKey}','${row.serviceKey}',${row.quantity})">✏️ แก้ไข</button>
        <button class="btn-sm btn-delete" onclick="deletePromo('${row.packageKey}','${row.serviceKey}',${row.quantity})">🗑️ ลบ</button>
      </td>
    </tr>
  `).join('');
}

function renderServicePills() {
  const container = elements.servicesList;
  if (!container) return;
  container.innerHTML = "";
  const services = Object.entries(state.config?.services || {});
  services.forEach(([key, svc]) => {
    const pill = document.createElement("span");
    pill.className = "pill";
    pill.innerHTML = `${svc.name || key} <small>(${key})</small>`;
    container.appendChild(pill);
  });
}

function renderCustomerPills() {
  const container = elements.customersList;
  if (!container) return;
  container.innerHTML = "";
  const customers = Object.entries(state.config?.customer_types || {});
  customers.forEach(([key, customer]) => {
    const pill = document.createElement("span");
    pill.className = "pill";
    pill.innerHTML = `${customer.name || key} <small>(${key})</small>`;
    container.appendChild(pill);
  });
}

function getPriceEntry({ itemKey, sizeKey, serviceKey, customerKey, packageKey }) {
  const sizeCfg = state.config?.items?.[itemKey]?.sizes?.[sizeKey];
  const pricing = sizeCfg?.pricing?.[serviceKey]?.[customerKey];
  if (!pricing) return null;
  return pricing[packageKey] || pricing.regular || Object.values(pricing)[0] || null;
}

function getServiceName(key) {
  return state.config?.services?.[key]?.name || key;
}

function getItemName(key) {
  return state.config?.items?.[key]?.name || key;
}

// ฟังก์ชันจัดการ Modal ราคา
window.editPrice = function(itemKey, sizeKey, serviceKey, customerKey, packageKey) {
  const price = getPriceEntry({ itemKey, sizeKey, serviceKey, customerKey, packageKey });
  
  document.getElementById('priceModalTitle').textContent = '✏️ แก้ไขราคา';
  document.getElementById('originalKey').value = `${itemKey}|${sizeKey}|${serviceKey}|${customerKey}|${packageKey}`;
  
  // Populate selectors
  populateModalSelectors();
  
  // Fill form
  document.getElementById('modalItemSelect').value = itemKey;
  populateSizesByItem(itemKey);
  document.getElementById('modalSizeSelect').value = sizeKey;
  document.getElementById('modalServiceSelect').value = serviceKey;
  document.getElementById('modalCustomerSelect').value = customerKey;
  document.getElementById('modalPackageInput').value = packageKey;
  document.getElementById('modalFullPrice').value = price?.full_price || '';
  document.getElementById('modalDiscount35').value = price?.discount_35 || '';
  document.getElementById('modalDiscount50').value = price?.discount_50 || '';
  
  elements.priceModal.classList.add('show');
};

window.closePriceModal = function() {
  elements.priceModal.classList.remove('show');
  elements.priceModalForm.reset();
};

window.deletePrice = async function(itemKey, sizeKey, serviceKey, customerKey, packageKey) {
  const confirmed = confirm(`คุณแน่ใจว่าต้องการลบราคานี้?\n\n${getItemName(itemKey)} (${sizeKey})\n${getServiceName(serviceKey)} - ${customerKey}\nแพคเกจ: ${packageKey}`);
  
  if (!confirmed) return;
  
  try {
    // Clone config and delete the price entry
    const newConfig = JSON.parse(JSON.stringify(state.config));
    const pricing = newConfig.items?.[itemKey]?.sizes?.[sizeKey]?.pricing?.[serviceKey]?.[customerKey];
    
    if (pricing && pricing[packageKey]) {
      delete pricing[packageKey];
      
      // If no packages left, clean up
      if (Object.keys(pricing).length === 0) {
        delete newConfig.items[itemKey].sizes[sizeKey].pricing[serviceKey][customerKey];
      }
      
      await adminFetch("/admin/config/pricing", {
        method: "PUT",
        body: JSON.stringify(newConfig),
      });
      
      showToast("ลบราคาสำเร็จแล้ว 🗑️");
      await refreshConfig();
    }
  } catch (err) {
    showToast(err.message, "error");
  }
};

elements.addNewPriceBtn?.addEventListener('click', function() {
  document.getElementById('priceModalTitle').textContent = '➕ เพิ่มราคาใหม่';
  document.getElementById('originalKey').value = '';
  populateModalSelectors();
  populateSizesByItem(''); // เคลียร์ size dropdown
  elements.priceModalForm.reset();
  elements.priceModal.classList.add('show');
});

elements.priceModalForm?.addEventListener('submit', async function(e) {
  e.preventDefault();
  
  const formData = new FormData(e.target);
  const data = {
    service_key: formData.get('serviceKey'),
    item_key: formData.get('itemKey'),
    size_key: formData.get('sizeKey'),
    customer_key: formData.get('customerKey'),
    package_key: formData.get('packageKey') || 'regular',
    price: {
      full_price: num(formData.get('fullPrice')),
      discount_35: num(formData.get('discount35')),
      discount_50: num(formData.get('discount50')),
    }
  };
  
  const itemName = getItemName(data.item_key);
  const serviceName = getServiceName(data.service_key);
  
  const confirmed = confirm(`ยืนยันการบันทึกราคา?\n\n${itemName} (${data.size_key})\n${serviceName} - ${data.customer_key}\n\nราคาเต็ม: ฿${data.price.full_price}\nลด 35%: ฿${data.price.discount_35}\nลด 50%: ฿${data.price.discount_50}`);
  
  if (!confirmed) return;
  
  try {
    await adminFetch("/admin/config/pricing/price", {
      method: "POST",
      body: JSON.stringify(data),
    });
    showToast("บันทึกราคาสำเร็จแล้ว 💰");
    await refreshConfig();
    closePriceModal();
  } catch (err) {
    showToast(err.message, "error");
  }
});

// ฟังก์ชันจัดการ Modal โปรโมชั่น
window.editPromo = function(packageKey, serviceKey, quantity) {
  const promo = state.config?.packages?.[packageKey]?.[serviceKey]?.[quantity];
  
  document.getElementById('promoModalTitle').textContent = '✏️ แก้ไขโปรโมชั่น';
  document.getElementById('promoOriginalKey').value = `${packageKey}|${serviceKey}|${quantity}`;
  
  // Populate dropdowns
  populatePromoModalSelectors();
  
  // Set values
  document.getElementById('modalPromoPackageSelect').value = packageKey;
  populatePromoServicesByPackage(packageKey);
  document.getElementById('modalPromoServiceSelect').value = serviceKey;
  populatePromoQuantitiesByService(packageKey, serviceKey);
  document.getElementById('modalPromoQuantitySelect').value = quantity;
  
  document.getElementById('modalPromoFullPrice').value = promo?.full_price || '';
  document.getElementById('modalPromoDiscount').value = promo?.discount || '';
  document.getElementById('modalPromoSalePrice').value = promo?.sale_price || '';
  document.getElementById('modalPromoPerItem').value = promo?.per_item || '';
  document.getElementById('modalPromoDeposit').value = promo?.deposit_min || '';
  
  // แสดง/ซ่อนฟิลด์มัดจำ: coupon ไม่มี, contract มี
  const depositGroup = document.getElementById('modalPromoDeposit').closest('.form-group');
  if (depositGroup) {
    depositGroup.style.display = packageKey === 'coupon' ? 'none' : 'block';
  }
  
  elements.promoModal.classList.add('show');
};

window.closePromoModal = function() {
  elements.promoModal.classList.remove('show');
  elements.promoModalForm.reset();
};

window.deletePromo = async function(packageKey, serviceKey, quantity) {
  const confirmed = confirm(`คุณแน่ใจว่าต้องการลบโปรโมชั่นนี้?\n\nแพคเกจ: ${packageKey}\nบริการ: ${getServiceName(serviceKey)}\nจำนวน: ${quantity} ครั้ง`);
  
  if (!confirmed) return;
  
  try {
    const newConfig = JSON.parse(JSON.stringify(state.config));
    
    if (newConfig.packages?.[packageKey]?.[serviceKey]?.[quantity]) {
      delete newConfig.packages[packageKey][serviceKey][quantity];
      
      // Clean up empty objects
      if (Object.keys(newConfig.packages[packageKey][serviceKey]).length === 0) {
        delete newConfig.packages[packageKey][serviceKey];
      }
      if (Object.keys(newConfig.packages[packageKey]).length === 0) {
        delete newConfig.packages[packageKey];
      }
      
      await adminFetch("/admin/config/pricing", {
        method: "PUT",
        body: JSON.stringify(newConfig),
      });
      
      showToast("ลบโปรโมชั่นสำเร็จแล้ว 🗑️");
      await refreshConfig();
    }
  } catch (err) {
    showToast(err.message, "error");
  }
};

elements.addNewPromoBtn?.addEventListener('click', function() {
  document.getElementById('promoModalTitle').textContent = '➕ เพิ่มโปรโมชั่นใหม่';
  document.getElementById('promoOriginalKey').value = '';
  elements.promoModalForm.reset();
  
  // Populate dropdowns for new entry
  populatePromoModalSelectors();
  document.getElementById('modalPromoServiceSelect').innerHTML = '<option value="">เลือกแพ็คเกจก่อน</option>';
  document.getElementById('modalPromoQuantitySelect').innerHTML = '<option value="">เลือกบริการก่อน</option>';
  
  // ซ่อนฟิลด์มัดจำเริ่มต้น
  const depositGroup = document.getElementById('modalPromoDeposit').closest('.form-group');
  if (depositGroup) {
    depositGroup.style.display = 'none';
  }
  
  elements.promoModal.classList.add('show');
});

elements.promoModalForm?.addEventListener('submit', async function(e) {
  e.preventDefault();
  
  const formData = new FormData(e.target);
  const data = {
    package_key: formData.get('packageKey'),
    service_key: formData.get('serviceKey'),
    quantity: parseInt(formData.get('quantity')),
    price: {
      full_price: num(formData.get('fullPrice')),
      discount: num(formData.get('discount')),
      sale_price: num(formData.get('salePrice')),
      per_item: num(formData.get('perItem')),
      deposit_min: num(formData.get('depositMin')),
    }
  };
  
  const serviceName = getServiceName(data.service_key);
  
  const confirmed = confirm(`ยืนยันการบันทึกโปรโมชั่น?\n\nแพคเกจ: ${data.package_key}\nบริการ: ${serviceName}\nจำนวน: ${data.quantity} ครั้ง\n\nราคาเต็ม: ฿${data.price.full_price}\nส่วนลด: ฿${data.price.discount}\nราคาขาย: ฿${data.price.sale_price}`);
  
  if (!confirmed) return;
  
  try {
    await adminFetch("/admin/config/pricing/promotion", {
      method: "POST",
      body: JSON.stringify(data),
    });
    showToast("บันทึกโปรโมชั่นสำเร็จแล้ว 🎁");
    await refreshConfig();
    closePromoModal();
  } catch (err) {
    showToast(err.message, "error");
  }
});

function populateModalSelectors() {
  // Populate item select
  const itemSelect = document.getElementById('modalItemSelect');
  const serviceSelect = document.getElementById('modalServiceSelect');
  const customerSelect = document.getElementById('modalCustomerSelect');
  
  if (itemSelect && state.config?.items) {
    itemSelect.innerHTML = '<option value="">เลือกสินค้า</option>' +
      Object.entries(state.config.items).map(([key, item]) => 
        `<option value="${key}">${item.name || key}</option>`
      ).join('');
  }
  
  if (serviceSelect && state.config?.services) {
    serviceSelect.innerHTML = '<option value="">เลือกบริการ</option>' +
      Object.entries(state.config.services).map(([key, service]) => 
        `<option value="${key}">${service.name || key}</option>`
      ).join('');
  }
  
  if (customerSelect && state.config?.customer_types) {
    customerSelect.innerHTML = '<option value="">เลือกประเภท</option>' +
      Object.entries(state.config.customer_types).map(([key, customer]) => 
        `<option value="${key}">${customer.name || key}</option>`
      ).join('');
  }
}

function populateSizesByItem(itemKey) {
  const sizeSelect = document.getElementById('modalSizeSelect');
  if (!sizeSelect || !itemKey || !state.config?.items?.[itemKey]) {
    if (sizeSelect) sizeSelect.innerHTML = '<option value="">เลือกสินค้าก่อน</option>';
    return;
  }
  
  const sizes = state.config.items[itemKey].sizes || {};
  sizeSelect.innerHTML = '<option value="">เลือกขนาด</option>' +
    Object.entries(sizes).map(([key, size]) => 
      `<option value="${key}">${size.name || key}</option>`
    ).join('');
}

// Event listener สำหรับเปลี่ยน item
document.getElementById('modalItemSelect')?.addEventListener('change', function(e) {
  populateSizesByItem(e.target.value);
});

function populatePromoModalSelectors() {
  const packageSelect = document.getElementById('modalPromoPackageSelect');
  const serviceSelect = document.getElementById('modalPromoServiceSelect');
  
  if (packageSelect && state.config?.packages) {
    packageSelect.innerHTML = '<option value="">เลือกแพ็คเกจ</option>' +
      Object.keys(state.config.packages).map(key => 
        `<option value="${key}">${key}</option>`
      ).join('');
  }
  
  if (serviceSelect && state.config?.services) {
    serviceSelect.innerHTML = '<option value="">เลือกแพ็คเกจก่อน</option>';
  }
}

function populatePromoServicesByPackage(packageKey) {
  const serviceSelect = document.getElementById('modalPromoServiceSelect');
  if (!serviceSelect || !packageKey || !state.config?.packages?.[packageKey]) {
    if (serviceSelect) serviceSelect.innerHTML = '<option value="">เลือกแพ็คเกจก่อน</option>';
    return;
  }
  
  const services = state.config.packages[packageKey] || {};
  // กรอง name และ aliases ออก เหลือแค่บริการจริง (disinfection, washing)
  const serviceKeys = Object.keys(services).filter(key => key !== 'name' && key !== 'aliases');
  serviceSelect.innerHTML = '<option value="">เลือกบริการ</option>' +
    serviceKeys.map(key => 
      `<option value="${key}">${state.config.services?.[key]?.name || key}</option>`
    ).join('');
}

function populatePromoQuantitiesByService(packageKey, serviceKey) {
  const quantitySelect = document.getElementById('modalPromoQuantitySelect');
  if (!quantitySelect || !packageKey || !serviceKey || !state.config?.packages?.[packageKey]?.[serviceKey]) {
    if (quantitySelect) quantitySelect.innerHTML = '<option value="">เลือกบริการก่อน</option>';
    return;
  }
  
  const quantities = state.config.packages[packageKey][serviceKey] || {};
  quantitySelect.innerHTML = '<option value="">เลือกจำนวน</option>' +
    Object.keys(quantities).map(key => 
      `<option value="${key}">${key} ชิ้น</option>`
    ).join('');
}

// Event listeners สำหรับเปลี่ยน package และ service
document.getElementById('modalPromoPackageSelect')?.addEventListener('change', function(e) {
  populatePromoServicesByPackage(e.target.value);
  document.getElementById('modalPromoQuantitySelect').innerHTML = '<option value="">เลือกบริการก่อน</option>';
  
  // แสดง/ซ่อนฟิลด์มัดจำตาม package
  const depositGroup = document.getElementById('modalPromoDeposit').closest('.form-group');
  if (depositGroup) {
    depositGroup.style.display = e.target.value === 'coupon' ? 'none' : 'block';
  }
});

document.getElementById('modalPromoServiceSelect')?.addEventListener('change', function(e) {
  const packageKey = document.getElementById('modalPromoPackageSelect').value;
  populatePromoQuantitiesByService(packageKey, e.target.value);
});

function formatMoney(value) {
  return `฿${numberFormatter.format(value)}`;
}

function showToast(message, type = "success") {
  const toast = elements.toast;
  toast.textContent = message;
  toast.className = `toast show ${type}`;
  clearTimeout(showToast.timeout);
  showToast.timeout = setTimeout(() => {
    toast.classList.remove("show");
  }, 3500);
}

// ปิด modal เมื่อคลิกนอก modal
window.onclick = function(event) {
  if (event.target == elements.priceModal) {
    closePriceModal();
  }
  if (event.target == elements.promoModal) {
    closePromoModal();
  }
};

if (state.token) {
  refreshConfig();
}

// ===== TAB SWITCHING =====
const tabBtns = document.querySelectorAll(".tab-btn");
const tabPanels = document.querySelectorAll(".tab-panel");

tabBtns.forEach((btn) => {
  btn.addEventListener("click", () => {
    const target = btn.dataset.tab;
    tabBtns.forEach((b) => b.classList.remove("active"));
    tabPanels.forEach((p) => (p.style.display = "none"));
    btn.classList.add("active");
    document.getElementById(`tab-${target}`).style.display = "";
    if (target === "conversations") {
      refreshConversations();
    }
  });
});

// ===== CONVERSATIONS =====
// Track which users have already triggered an alert (reset when resolved)
const alertedHumanUsers = new Set();

// Generate an alert beep using Web Audio API (no audio file needed)
function playAlertBeep() {
  try {
    const ctx = new (window.AudioContext || window.webkitAudioContext)();
    let t = ctx.currentTime;
    [880, 1100, 880].forEach((freq, i) => {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.connect(gain);
      gain.connect(ctx.destination);
      osc.type = "square";
      osc.frequency.value = freq;
      gain.gain.setValueAtTime(0.25, t + i * 0.15);
      gain.gain.exponentialRampToValueAtTime(0.001, t + i * 0.15 + 0.13);
      osc.start(t + i * 0.15);
      osc.stop(t + i * 0.15 + 0.13);
    });
  } catch (_) { /* audio not available */ }
}

const convState = {
  conversations: [],
  selectedUserId: null,
  pollTimer: null,
  forceScrollBottom: false, // set true when admin opens a conversation
  lastReadTimestamp: {}, // userId -> ISO timestamp of last message seen
};

const convEls = {
  list: document.getElementById("convList"),
  threadHeader: document.getElementById("convThreadHeader"),
  threadUserId: document.getElementById("convThreadUserId"),
  statusBadge: document.getElementById("convStatusBadge"),
  messages: document.getElementById("convMessages"),
  replyBar: document.getElementById("convReplyBar"),
  replyInput: document.getElementById("convReplyInput"),
  replyBtn: document.getElementById("convReplyBtn"),
  btnTakeover: document.getElementById("btnTakeover"),
  btnRelease: document.getElementById("btnRelease"),
  refreshBtn: document.getElementById("refreshConversationsBtn"),
};

const refreshConversations = async () => {
  try {
    const data = await adminFetch("/admin/conversations");
    // Sort: wants_human first, then by last_seen desc
    data.sort((a, b) => {
      if (a.wants_human !== b.wants_human) return a.wants_human ? -1 : 1;
      return (b.last_seen || "").localeCompare(a.last_seen || "");
    });
    convState.conversations = data;
    renderConvList();
    if (convState.selectedUserId) {
      refreshConvThread(convState.selectedUserId);
    }

    // --- Alert: detect new customers wanting a human agent ---
    const pendingHuman = data.filter((c) => c.wants_human && !c.takeover);
    const newAlerts = pendingHuman.filter((c) => !alertedHumanUsers.has(c.user_id));
    if (newAlerts.length > 0) {
      newAlerts.forEach((c) => alertedHumanUsers.add(c.user_id));
      playAlertBeep();
      const notifBody = newAlerts.length === 1
        ? `ลูกค้า ...${newAlerts[0].user_id.slice(-8)} ต้องการคุยกับเจ้าหน้าที่`
        : `ลูกค้า ${newAlerts.length} รายต้องการคุยกับเจ้าหน้าที่`;
      if (Notification.permission === "granted") {
        new Notification("🆘 NCS Admin", { body: notifBody, icon: "" });
      } else if (Notification.permission !== "denied") {
        Notification.requestPermission().then((perm) => {
          if (perm === "granted") new Notification("🆘 NCS Admin", { body: notifBody });
        });
      }
    }
    // Remove resolved users from the alerted set so they can re-alert if needed
    data.filter((c) => !c.wants_human || c.takeover).forEach((c) => alertedHumanUsers.delete(c.user_id));
    // Update page title with pending count
    const pendingCount = pendingHuman.length;
    document.title = pendingCount > 0 ? `(🔴 ${pendingCount}) NCS Admin` : "NCS Admin";
  } catch (err) {
    // silently ignore polling errors
  }
};

function renderConvList() {
  const list = convEls.list;
  if (!list) return;
  if (!convState.conversations.length) {
    list.innerHTML = '<p class="conv-empty">ยังไม่มีบทสนทนา</p>';
    return;
  }
  list.innerHTML = convState.conversations
    .map((c) => {
      const isActive = c.user_id === convState.selectedUserId;
      const shortId = c.user_id ? c.user_id.slice(-8) : "?";
      const displayName = c.nickname || c.display_name || `…${shortId}`;
      const alertBadge = c.wants_human ? '<span class="alert-badge">🆘 ขอคุย</span>' : "";
      const takeoverBadge = c.takeover
        ? '<span class="mode-badge human">👤 Admin</span>'
        : '<span class="mode-badge ai">🤖 AI</span>';
      const lastMsg = c.last_message
        ? escapeHtml(c.last_message).slice(0, 55) + (c.last_message.length > 55 ? "…" : "")
        : "<em>ไม่มีข้อความ</em>";
      const msgCount = `<small>${c.message_count} ข้อความ</small>`;
      const lastSeen = c.last_seen ? `<small>${c.last_seen.replace("T", " ")}</small>` : "";
      // Unread badge: new messages since admin last read this conversation
      const lastRead = convState.lastReadTimestamp[c.user_id];
      const hasUnread = !isActive && c.last_seen && (!lastRead || c.last_seen > lastRead);
      const unreadBadge = hasUnread ? '<span class="unread-badge">●</span>' : "";
      return `<div class="conv-item${isActive ? " active" : ""}${hasUnread ? " has-unread" : ""}" data-uid="${escapeAttr(c.user_id)}">
        <div class="conv-item-top">
          <span class="conv-uid">${escapeHtml(displayName)}</span>
          ${unreadBadge}${alertBadge}${takeoverBadge}
        </div>
        <div class="conv-item-preview">${lastMsg}</div>
        <div class="conv-item-meta">${msgCount} ${lastSeen}</div>
      </div>`;
    })
    .join("");

  list.querySelectorAll(".conv-item").forEach((el) => {
    el.addEventListener("click", () => selectConversation(el.dataset.uid));
  });
}

async function selectConversation(userId) {
  convState.selectedUserId = userId;
  convState.forceScrollBottom = true; // always scroll to bottom when opening a conversation
  renderConvList(); // re-render to update active
  await refreshConvThread(userId);
  // Mark all messages as read when opening conversation
  const conv = convState.conversations.find(c => c.user_id === userId);
  if (conv && conv.last_seen) {
    convState.lastReadTimestamp[userId] = conv.last_seen;
  }
  renderConvList(); // re-render to clear unread badge
}

async function refreshConvThread(userId) {
  try {
    const conv = await adminFetch(`/admin/conversations/${encodeURIComponent(userId)}`);
    renderConvThread(conv);
  } catch (err) {
    // ignore
  }
}

function renderConvThread(conv) {
  if (!conv) return;

  // Header
  if (convEls.threadHeader) convEls.threadHeader.style.display = "";
  if (convEls.threadUserId) {
    const displayName = conv.nickname || conv.display_name || `…${conv.user_id ? conv.user_id.slice(-8) : "?"}`;
    const shortId = conv.user_id ? conv.user_id.slice(-8) : "?";
    const lineNameHint = conv.display_name && !conv.nickname
      ? ` <small style="opacity:.6">(LINE: ${escapeHtml(conv.display_name)})</small>` : "";
    convEls.threadUserId.innerHTML =
      `<strong>${escapeHtml(displayName)}</strong>${lineNameHint}
       <button class="btn-edit-nickname" title="ตั้งชื่อเล่น" onclick="openNicknameEditor('${escapeAttr(conv.user_id)}','${escapeAttr(conv.nickname || '')}')">✏️</button>`;
  }

  const badge = convEls.statusBadge;
  if (badge) {
    if (conv.wants_human && !conv.takeover) {
      badge.textContent = "🆘 ลูกค้าขอคุยกับคน";
      badge.className = "conv-status-badge alert";
    } else if (conv.takeover) {
      badge.textContent = "👤 Admin กำลังตอบ";
      badge.className = "conv-status-badge human";
    } else {
      badge.textContent = "🤖 AI กำลังตอบ";
      badge.className = "conv-status-badge ai";
    }
  }

  // Takeover/release buttons
  if (convEls.btnTakeover) {
    convEls.btnTakeover.style.display = conv.takeover ? "none" : "";
  }
  if (convEls.btnRelease) {
    convEls.btnRelease.style.display = conv.takeover ? "" : "none";
  }

  // Reply bar - always visible
  if (convEls.replyBar) convEls.replyBar.style.display = "";

  // Messages
  const msgs = conv.messages || [];
  if (!msgs.length) {
    convEls.messages.innerHTML = '<p class="conv-empty" style="margin-top:60px">ยังไม่มีข้อความ</p>';
    return;
  }
  convEls.messages.innerHTML = msgs
    .map((m) => {
      const cls =
        m.role === "customer" ? "bubble-customer" : m.role === "admin" ? "bubble-admin" : "bubble-ai";
      const label =
        m.role === "customer" ? "👤 ลูกค้า" : m.role === "admin" ? "👨\u200d💼 Admin" : "🤖 AI";
      const time = m.timestamp ? m.timestamp.replace("T", " ") : "";
      return `<div class="msg-row ${m.role}">
        <div class="bubble ${cls}">
          <div class="bubble-label">${label} <span class="bubble-time">${escapeHtml(time)}</span></div>
          <div class="bubble-text">${escapeHtml(m.text)}</div>
        </div>
      </div>`;
    })
    .join("");
  // Scroll to bottom only when admin just opened this conversation OR is already near the bottom.
  // This prevents the 8-second poll from jumping admin back to the bottom while reading old messages.
  const msgEl = convEls.messages;
  const isNearBottom = msgEl.scrollHeight - msgEl.scrollTop - msgEl.clientHeight < 120;
  if (convState.forceScrollBottom || isNearBottom) {
    msgEl.scrollTop = msgEl.scrollHeight;
    // Mark as read when we scroll to bottom
    if (conv.last_seen) convState.lastReadTimestamp[conv.user_id] = conv.last_seen;
    hideNewMsgBanner();
  } else {
    // Admin is scrolled up — show new message banner if there are unread messages
    const lastRead = convState.lastReadTimestamp[conv.user_id];
    const hasNewMsgs = msgs.some(m => m.role !== 'admin' && m.timestamp && (!lastRead || m.timestamp > lastRead));
    if (hasNewMsgs) showNewMsgBanner(msgEl);
  }
  convState.forceScrollBottom = false;
}

function showNewMsgBanner(msgEl) {
  let banner = document.getElementById('newMsgBanner');
  if (!banner) {
    banner = document.createElement('div');
    banner.id = 'newMsgBanner';
    banner.className = 'new-msg-banner';
    banner.textContent = '💬 มีข้อความใหม่ — คลิกเพื่อดู';
    banner.addEventListener('click', () => {
      msgEl.scrollTop = msgEl.scrollHeight;
      const userId = convState.selectedUserId;
      const conv = convState.conversations.find(c => c.user_id === userId);
      if (conv && conv.last_seen) convState.lastReadTimestamp[userId] = conv.last_seen;
      hideNewMsgBanner();
      renderConvList();
    });
    msgEl.parentElement.appendChild(banner);
  } else {
    // Re-attach click so msgEl reference is always current
    const old = banner.cloneNode(true);
    banner.parentElement?.replaceChild(old, banner);
    old.addEventListener('click', () => {
      msgEl.scrollTop = msgEl.scrollHeight;
      const userId = convState.selectedUserId;
      const conv = convState.conversations.find(c => c.user_id === userId);
      if (conv && conv.last_seen) convState.lastReadTimestamp[userId] = conv.last_seen;
      hideNewMsgBanner();
      renderConvList();
    });
  }
  banner = document.getElementById('newMsgBanner');
  if (banner) banner.style.display = '';
}

function hideNewMsgBanner() {
  const banner = document.getElementById('newMsgBanner');
  if (banner) banner.style.display = 'none';
}

convEls.btnTakeover?.addEventListener("click", async () => {
  if (!convState.selectedUserId) return;
  try {
    await adminFetch(`/admin/conversations/${encodeURIComponent(convState.selectedUserId)}/takeover`, {
      method: "POST",
    });
    showToast("หยุด AI แล้ว - Admin รับสายแล้ว 👤");
    await refreshConvThread(convState.selectedUserId);
    await refreshConversations();
  } catch (err) {
    showToast(err.message, "error");
  }
});

convEls.btnRelease?.addEventListener("click", async () => {
  if (!convState.selectedUserId) return;
  try {
    await adminFetch(`/admin/conversations/${encodeURIComponent(convState.selectedUserId)}/release`, {
      method: "POST",
    });
    showToast("คืนให้ AI แล้ว 🤖");
    await refreshConvThread(convState.selectedUserId);
    await refreshConversations();
  } catch (err) {
    showToast(err.message, "error");
  }
});

async function sendAdminReply() {
  const msg = convEls.replyInput?.value?.trim();
  if (!msg || !convState.selectedUserId) return;
  try {
    await adminFetch(`/admin/conversations/${encodeURIComponent(convState.selectedUserId)}/reply`, {
      method: "POST",
      body: JSON.stringify({ message: msg }),
    });
    convEls.replyInput.value = "";
    showToast("ส่งข้อความแล้ว ✅");
    await refreshConvThread(convState.selectedUserId);
  } catch (err) {
    showToast(err.message, "error");
  }
}

convEls.replyBtn?.addEventListener("click", sendAdminReply);

convEls.replyInput?.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && e.ctrlKey) {
    e.preventDefault();
    sendAdminReply();
  }
});

convEls.refreshBtn?.addEventListener("click", refreshConversations);

// ===== NICKNAME EDITOR =====
window.openNicknameEditor = function (userId, currentNickname) {
  const input = prompt("ตั้งชื่อเล่นลูกค้า (เว้นว่างเพื่อลบ):", currentNickname);
  if (input === null) return; // cancelled
  adminFetch(`/admin/conversations/${encodeURIComponent(userId)}/nickname`, {
    method: "POST",
    body: JSON.stringify({ nickname: input.trim() }),
  })
    .then(() => {
      showToast(input.trim() ? `ตั้งชื่อ "${input.trim()}" แล้ว ✅` : "ลบชื่อเล่นแล้ว");
      refreshConversations();
      if (convState.selectedUserId === userId) refreshConvThread(userId);
    })
    .catch((err) => showToast(err.message, "error"));
};

// Auto-poll conversations every 8 seconds while on conversations tab
setInterval(() => {
  const activeTab = document.querySelector(".tab-btn.active");
  if (activeTab?.dataset?.tab === "conversations") {
    refreshConversations();
  }
}, 8000);

// Helper: escape HTML
function escapeHtml(str) {
  if (!str) return "";
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/\n/g, "<br>");
}
function escapeAttr(str) {
  if (!str) return "";
  return String(str).replace(/"/g, "&quot;");
}
