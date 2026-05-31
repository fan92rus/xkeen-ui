let sessionToken = null;
let csrfToken = null;

document.getElementById('loginForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    const form = e.target;
    const password = form.password.value;
    try {
        const response = await fetch('/api/auth/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ password })
        });
        const data = await response.json();
        if (data.ok) {
            csrfToken = data.csrf_token;
            localStorage.setItem('csrfToken', data.csrf_token);
            if (data.require_password_change) {
                document.getElementById('loginForm').classList.add('hidden');
                document.getElementById('changePasswordForm').classList.add('active');
                document.getElementById('changePasswordForm').querySelector('[name="current_password"]').value = password;
                showToast(data.message || 'Смените стандартный пароль', 'warning');
            } else {
                window.location.href = '/';
            }
        } else {
            showToast(data.error || 'Ошибка входа', 'error');
        }
    } catch (error) { showToast('Ошибка сети', 'error'); }
});

document.getElementById('newPassword').addEventListener('input', (e) => {
    const pw = e.target.value;
    const bar = document.getElementById('passwordStrength');
    const hint = document.getElementById('passwordRequirements');
    let s = 0;
    if (pw.length >= 8) s++;
    if (pw.length >= 12) s++;
    if (/[a-z]/.test(pw) && /[A-Z]/.test(pw)) s++;
    if (/\d/.test(pw)) s++;
    if (/[^a-zA-Z0-9]/.test(pw)) s++;
    bar.className = 'password-strength';
    if (!pw.length) { bar.style.width = '0'; return; }
    if (s <= 2) { bar.classList.add('strength-weak'); bar.style.width = '33%'; hint.textContent = 'Слабый'; }
    else if (s <= 3) { bar.classList.add('strength-medium'); bar.style.width = '66%'; hint.textContent = 'Средний'; }
    else { bar.classList.add('strength-strong'); bar.style.width = '100%'; hint.textContent = '✓ Надёжный'; }
});

document.getElementById('changePasswordForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    const form = e.target;
    const cur = form.current_password.value, nw = form.new_password.value, cnf = form.confirm_password.value;
    if (nw.length < 8) { showToast('Минимум 8 символов', 'error'); return; }
    if (nw === cur) { showToast('Пароль должен отличаться', 'error'); return; }
    if (nw !== cnf) { showToast('Пароли не совпадают', 'error'); return; }
    const btn = document.getElementById('changePasswordBtn');
    btn.disabled = true; btn.textContent = 'Изменение...';
    try {
        const r = await fetch('/api/auth/change-password', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
            body: JSON.stringify({ current_password: cur, new_password: nw })
        });
        const d = await r.json();
        if (d.ok) {
            showToast('Пароль изменён', 'success');
            setTimeout(async () => {
                const lr = await fetch('/api/auth/login', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ password: nw })
                });
                const ld = await lr.json();
                if (ld.ok) { localStorage.setItem('csrfToken', ld.csrf_token); window.location.href = '/'; }
                else { document.getElementById('changePasswordForm').classList.remove('active'); document.getElementById('loginForm').classList.remove('hidden'); }
            }, 800);
        } else { showToast(d.error || 'Ошибка', 'error'); btn.disabled = false; btn.textContent = 'Изменить пароль'; }
    } catch { showToast('Ошибка сети', 'error'); btn.disabled = false; btn.textContent = 'Изменить пароль'; }
});

function showToast(message, type = 'info') {
    const t = document.getElementById('toast');
    t.textContent = message; t.className = 'toast ' + type; t.classList.remove('hidden');
    setTimeout(() => t.classList.add('hidden'), 4000);
}
