/**
 * 预览页面的首页部分。
 */
export function Hero() {
  const { t } = useTranslation()
  return (
    <section className='relative px-6 py-20 md:py-28'>
      <div className='mx-auto max-w-6xl'>
        <div className='mb-8 flex items-center justify-center gap-4'>
          <CheeseArt className='h-16 w-16 text-amber-500' />
          <h1 className='text-5xl font-bold tracking-tight text-amber-600 dark:text-amber-400'>
            New API
          </h1>
        </div>
        <p className='mx-auto mb-10 max-w-2xl text-center text-lg text-muted-foreground'>
          {t('Build, test and deploy AI models and applications.')}
        </p>
        <div className='flex flex-wrap justify-center gap-4'>
          <Button size='lg' className='px-8'>
            {t('Get Started')}
          </Button>
          <Button size='lg' variant='outline' className='px-8'>
            {t('Learn More')}
          </Button>
        </div>
      </div>
    </section>
  )
}

/**
 * 特性展示部分。
 */
export function Features() {
  const { t } = useTranslation()
  return (
    <section className='px-6 py-16 md:py-20'>
      <div className='mx-auto max-w-6xl'>
        <h2 className='mb-12 text-center text-3xl font-bold tracking-tight md:text-4xl'>
          {t('Features')}
        </h2>
        <div className='grid gap-8 md:grid-cols-3'>
          <FeatureItem
            icon='lucide:box'
            title={t('Simple')}
            description={t('Simple and easy to use interface.')}
          />
          <FeatureItem
            icon='lucide:fast-forward'
            title={t('Fast')}
            description={t('Fast performance and quick deployment.')}
          />
          <FeatureItem
            icon='lucide:shield-check'
            title={t('Reliable')}
            description={t('Reliable and stable service.')}
          />
        </div>
      </div>
    </section>
  )
}

/**
 * 产品介绍 / 使用方法部分。
 */
export function HowItWorks() {
  const { t } = useTranslation()
  return (
    <section className='px-6 py-16 md:py-20'>
      <div className='mx-auto max-w-6xl'>
        <h2 className='mb-12 text-center text-3xl font-bold tracking-tight md:text-4xl'>
          {t('How It Works')}
        </h2>
        <div className='grid gap-8 md:grid-cols-3'>
          <div className='flex flex-col items-center text-center'>
            <div className='mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-primary/10 text-primary'>
              <Box className='h-8 w-8' />
            </div>
            <h3 className='mb-2 text-xl font-semibold'>{t('Build')}</h3>
            <p className='text-muted-foreground'>
              {t('Deploy your models and applications quickly.')}
            </p>
          </div>
          <div className='flex flex-col items-center text-center'>
            <div className='mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-primary/10 text-primary'>
              <FlaskConical className='h-8 w-8' />
            </div>
            <h3 className='mb-2 text-xl font-semibold'>{t('Test')}</h3>
            <p className='text-muted-foreground'>
              {t('Test and optimize your models in real-time.')}
            </p>
          </div>
          <div className='flex flex-col items-center text-center'>
            <div className='mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-primary/10 text-primary'>
              <Cloud className='h-8 w-8' />
            </div>
            <h3 className='mb-2 text-xl font-semibold'>{t('Deploy')}</h3>
            <p className='text-muted-foreground'>
              {t('Deploy to production with one click.')}
            </p>
          </div>
        </div>
      </div>
    </section>
  )
}

/**
 * 统计数据展示部分。
 */
export function Stats() {
  const { t } = useTranslation()
  return (
    <section className='px-6 py-16 md:py-20'>
      <div className='mx-auto max-w-6xl'>
        <h2 className='mb-12 text-center text-3xl font-bold tracking-tight md:text-4xl'>
          {t('Stats')}
        </h2>
        <div className='grid gap-8 md:grid-cols-4'>
          <StatItem
            icon='lucide:cpu'
            value='10k+'
            label={t('Models')}
          />
          <StatItem
            icon='lucide:cloud-upload'
            value='1M+'
            label={t('Deployments')}
          />
          <StatItem
            icon='lucide:activity'
            value='99.9%'
            label={t('Uptime')}
          />
          <StatItem
            icon='lucide:users'
            value='5k+'
            label={t('Users')}
          />
        </div>
      </div>
    </section>
  )
}