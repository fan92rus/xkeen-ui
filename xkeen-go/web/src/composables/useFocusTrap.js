// composables/useFocusTrap.js — Focus trap composable for modal dialogs.
//
// When a modal is open, Tab key cycles within the modal instead of
// reaching background elements.  Also focuses the first focusable element
// on open and restores focus to the previously active element on close.

import { onUnmounted, nextTick, watch } from 'vue';

const FOCUSABLE_SELECTOR = [
    'a[href]', 'button:not([disabled])', 'input:not([disabled])',
    'select:not([disabled])', 'textarea:not([disabled])',
    '[tabindex]:not([tabindex="-1"])',
].join(',');

/**
 * @param {import('vue').Ref<HTMLElement|null>} elRef - Ref to the modal container.
 * @param {import('vue').Ref<boolean>|import('vue').ComputedRef<boolean>} isOpenRef - Ref controlling visibility.
 */
export function useFocusTrap(elRef, isOpenRef) {
    let previouslyFocused = null;

    function getFocusable() {
        const el = elRef.value;
        if (!el) return [];
        return Array.from(el.querySelectorAll(FOCUSABLE_SELECTOR));
    }

    function onKeydown(e) {
        if (e.key !== 'Tab' || !isOpenRef.value) return;
        const focusable = getFocusable();
        if (focusable.length === 0) return;

        const first = focusable[0];
        const last = focusable[focusable.length - 1];

        if (e.shiftKey) {
            if (document.activeElement === first || !elRef.value?.contains(document.activeElement)) {
                e.preventDefault();
                last.focus();
            }
        } else {
            if (document.activeElement === last || !elRef.value?.contains(document.activeElement)) {
                e.preventDefault();
                first.focus();
            }
        }
    }

    watch(isOpenRef, async (open) => {
        if (open) {
            previouslyFocused = document.activeElement;
            document.addEventListener('keydown', onKeydown);
            await nextTick();
            const focusable = getFocusable();
            if (focusable.length > 0) {
                focusable[0].focus();
            }
        } else {
            document.removeEventListener('keydown', onKeydown);
            if (previouslyFocused && previouslyFocused.focus) {
                previouslyFocused.focus();
                previouslyFocused = null;
            }
        }
    });

    onUnmounted(() => {
        document.removeEventListener('keydown', onKeydown);
    });
}
