import type { Env, EmailEvent } from './types.js';
import { processEmailEvent } from './router.js';

// Queue consumer handler — receives batches of email events from fly.io nodes.
export default {
  async queue(batch: MessageBatch<EmailEvent>, env: Env): Promise<void> {
    for (const msg of batch.messages) {
      try {
        await processEmailEvent(msg.body, env);
        msg.ack();
      } catch (err) {
        console.error('queue: failed to process message', msg.body.message_id, err);
        msg.retry();
      }
    }
  },
} satisfies ExportedHandler<Env>;
