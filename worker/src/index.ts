import type { Env, EmailEvent } from './types.js';
import { processEmailEvent } from './router.js';

// Queue consumer handler — receives batches of email events from fly.io nodes.
export default {
  async queue(batch: MessageBatch<unknown>, env: Env): Promise<void> {
    for (const msg of batch.messages) {
      const event = msg.body as EmailEvent;
      try {
        await processEmailEvent(event, env);
        msg.ack();
      } catch (err) {
        console.error('queue: failed to process message', event.message_id, err);
        msg.retry();
      }
    }
  },
} satisfies ExportedHandler<Env>;
