// components/service.js - Service status and control buttons

document.addEventListener('alpine:init', () => {
    Alpine.data('service', function() {
        return {
            interval: null,

            init() {
                this.$store.app.fetchServiceStatus();
                this.startPolling();
            },

            destroy() {
                this.stopPolling();
            },

            startPolling() {
                this.interval = setInterval(() => {
                    this.$store.app.fetchServiceStatus();
                }, 5000);
            },

            stopPolling() {
                if (this.interval) {
                    clearInterval(this.interval);
                    this.interval = null;
                }
            },

            start() {
                this.$store.app.startService();
            },

            stop() {
                this.$store.app.stopService();
            }
        };
    });
});
