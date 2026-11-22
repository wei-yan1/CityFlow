const form = document.querySelector('#registrationForm');
const message = document.querySelector('#message');
const submitButton = document.querySelector('#submitRegistration');
const successPanel = document.querySelector('#successPanel');
const successReference = document.querySelector('#successReference');
const skillsInput = form.querySelector('[name="skills"]');

function selectedSkills() {
  return [...form.querySelectorAll('.skill-chip[aria-pressed="true"]')].map(button => button.dataset.skill);
}

form.querySelectorAll('.skill-chip').forEach(button => {
  button.addEventListener('click', () => {
    const selected = button.getAttribute('aria-pressed') === 'true';
    if (selected && form.querySelectorAll('.skill-chip[aria-pressed="true"]').length === 1) return;
    button.setAttribute('aria-pressed', String(!selected));
    skillsInput.value = selectedSkills().join(',');
  });
});

function splitSkills(value) {
  const aliases = { '配送': 'DELIVERY', '代取': 'PICKUP', '代购': 'PURCHASE', '跑腿': 'ERRAND', '其他': 'OTHER' };
  return String(value || '').split(/[,，、]/).map(item => item.trim()).filter(Boolean).map(item => aliases[item] || item.toUpperCase());
}

form.addEventListener('submit', async event => {
  event.preventDefault();
  message.textContent = '';
  if (!form.reportValidity()) return;

  const data = new FormData(form);
  const amount = Number(data.get('paid_amount'));
  if (!Number.isFinite(amount) || amount < 0.01) {
    message.textContent = '请输入有效的实付金额。';
    return;
  }
  const paidAmountFen = Math.round(amount * 100);
  if (paidAmountFen < 1) {
    message.textContent = '请输入有效的实付金额。';
    return;
  }
  const skills = splitSkills(data.get('skills'));
  if (!skills.length) {
    message.textContent = '请至少选择一个服务类型。';
    return;
  }
  const maxDistanceMeters = Number(data.get('max_distance_meters'));
  const maxConcurrentTasks = Number(data.get('max_concurrent_tasks'));
  if (!Number.isInteger(maxDistanceMeters) || maxDistanceMeters < 1 || maxDistanceMeters > 100000) {
    message.textContent = '最大服务距离必须是 1 到 100000 之间的整数。';
    return;
  }
  if (!Number.isInteger(maxConcurrentTasks) || maxConcurrentTasks < 1 || maxConcurrentTasks > 20) {
    message.textContent = '同时接单上限必须是 1 到 20 之间的整数。';
    return;
  }

  const body = {
    email: String(data.get('email') || '').trim(),
    password: String(data.get('password') || ''),
    display_name: String(data.get('display_name') || '').trim(),
    phone: String(data.get('phone') || '').trim(),
    vehicle_type: String(data.get('vehicle_type') || '').trim(),
    skills,
    service_area: String(data.get('service_area') || '').trim(),
    max_distance_meters: maxDistanceMeters,
    max_concurrent_tasks: maxConcurrentTasks,
    paid_amount_fen: paidAmountFen,
    payment_reference: String(data.get('payment_reference') || '').trim(),
    payment_note: String(data.get('payment_note') || '').trim(),
  };

  submitButton.disabled = true;
  submitButton.textContent = '正在提交…';
  try {
    const response = await fetch('/api/v1/rider-registrations', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(friendlyError(result.error, response.status));
    form.hidden = true;
    successPanel.hidden = false;
    const reference = String(result.id || '').slice(0, 8).toUpperCase();
    successReference.textContent = reference ? `认证编号：${reference}。状态：待核验。` : '状态：待核验。';
    successPanel.scrollIntoView({ behavior: 'smooth', block: 'start' });
  } catch (error) {
    message.textContent = error.message;
  } finally {
    submitButton.disabled = false;
    submitButton.textContent = '提交认证申请';
  }
});

document.querySelector('#backToLogin').addEventListener('click', () => {
  location.replace('/login');
});
