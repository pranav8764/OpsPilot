'use client';

import { useEffect, useState } from 'react';
import { useAuth, useUser } from '@clerk/nextjs';
import { syncUser } from '@/lib/api';

export default function SyncUser() {
  const { getToken } = useAuth();
  const { user } = useUser();
  const [synced, setSynced] = useState(false);

  useEffect(() => {
    async function performSync() {
      if (!user || synced) return;

      try {
        const token = await getToken();
        if (!token) return;

        await syncUser(token, {
          email: user.primaryEmailAddress?.emailAddress || '',
          name: user.fullName || '',
          avatar_url: user.imageUrl || '',
        });
        setSynced(true);
      } catch (error) {
        console.error('Failed to sync user with OpsPilot API:', error);
      }
    }

    performSync();
  }, [user, getToken, synced]);

  return null; // Silent component
}
