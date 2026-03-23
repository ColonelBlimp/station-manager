import { toast } from '@zerodevx/svelte-toast';

export const showToast = {
    SUCCESS: (message: string, duration: number = 2000): void => {
        toast.pop(0); // clear all other toasts
        toast.push(message, {
            theme: {
                '--toastBackground': '#D1FAE5',
                '--toastColor': '#047857',
                '--toastBarBackground': '#047857',
                '--toastBarColor': '#fff',
            },
            duration: duration,
        });
    },
    ERROR: (message: string, duration: number = 3000): void => {
        toast.pop(0); // clear all other toasts
        toast.push(message, {
            theme: {
                '--toastBackground': '#FED7D7',
                '--toastColor': '#9B2C2C',
                '--toastBarBackground': '#C53030',
                '--toastBarColor': '#fff',
            },
            duration: duration,
        });
    },
    WARN: (message: string, duration: number = 3000): void => {
        toast.pop(0); // clear all other toasts
        toast.push(message, {
            theme: {
                '--toastBackground': '#FEF3C7',
                '--toastColor': '#B45309',
                '--toastBarBackground': '#B45309',
                '--toastBarColor': '#fff',
            },
            duration: duration,
        });
    },
    INFO: (message: string, duration: number = 3000): void => {
        toast.pop(0); // clear all other toasts
        toast.push(message, {
            theme: {
                '--toastBackground': '#E0F2FE',
                '--toastColor': '#1E3A8A',
                '--toastBarBackground': '#1E3A8A',
                '--toastBarColor': '#fff',
            },
            duration: duration,
        });
    },
    INFOSTICKY: (message: string): void => {
        toast.push(message, {
            theme: {
                '--toastBackground': '#E0F2FE',
                '--toastColor': '#1E3A8A',
                '--toastBarBackground': '#1E3A8A',
                '--toastBarColor': '#fff',
            },
            initial: 0, // No expiry
            dismissable: false, // Non-dismissable
        });
    },
};
