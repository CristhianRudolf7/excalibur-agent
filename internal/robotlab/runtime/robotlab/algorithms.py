"""Conservative CPU training defaults; caller hyperparameters override these."""
from __future__ import annotations

DEFAULTS = {
    'PPO': {'n_steps': 256, 'batch_size': 64, 'learning_rate': 0.0003, 'n_epochs': 4},
    'SAC': {'buffer_size': 100_000, 'learning_starts': 100, 'batch_size': 128, 'learning_rate': 0.0003},
    'TD3': {'buffer_size': 100_000, 'learning_starts': 100, 'batch_size': 128, 'learning_rate': 0.001},
    'A2C': {'n_steps': 5, 'learning_rate': 0.0007},
    'DQN': {'buffer_size': 50_000, 'learning_starts': 100, 'batch_size': 32, 'learning_rate': 0.0001},
}
