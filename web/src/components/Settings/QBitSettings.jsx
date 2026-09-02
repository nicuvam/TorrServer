import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Button,
  CircularProgress,
  FormControlLabel,
  FormGroup,
  FormHelperText,
  IconButton,
  List,
  ListItem,
  ListItemSecondaryAction,
  ListItemText,
  Switch,
  TextField,
  Typography,
} from '@material-ui/core'
import DeleteIcon from '@material-ui/icons/Delete'
import axios from 'axios'
import { qbitCategoriesHost, qbitTestHost } from 'utils/Hosts'

import { SecondarySettingsContent, SettingSectionLabel } from './style'

export default function QBitSettings({ settings, updateSettings }) {
  const { t } = useTranslation()
  const { QBitSettings } = settings || {}
  const {
    Enabled = false,
    URL = '',
    Username = '',
    Password = '',
    Tags = '',
    SavePath = '',
    SequentialDownload = false,
    FirstLastPiecePrio = false,
    PathMaps = [],
    AutoLocal = false,
    AutoImport = false,
  } = QBitSettings || {}

  const [newFrom, setNewFrom] = useState('')
  const [newTo, setNewTo] = useState('')
  const [testing, setTesting] = useState(false)
  const [creatingCategories, setCreatingCategories] = useState(false)
  const [testResult, setTestResult] = useState(null)

  const handleChange = (field, value) => {
    updateSettings({
      QBitSettings: {
        ...QBitSettings,
        [field]: value,
      },
    })
  }

  const handleAddPathMap = () => {
    if (newFrom && newTo) {
      updateSettings({
        QBitSettings: {
          ...QBitSettings,
          PathMaps: [...(PathMaps || []), { From: newFrom, To: newTo }],
        },
      })
      setNewFrom('')
      setNewTo('')
    }
  }

  const handleDeletePathMap = index => {
    const newMaps = [...(PathMaps || [])]
    newMaps.splice(index, 1)
    handleChange('PathMaps', newMaps)
  }

  const handleTest = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const { data } = await axios.post(qbitTestHost(), {
        url: URL,
        username: Username,
        password: Password,
      })
      if (data.success) {
        setTestResult({ success: true, msg: `${t('QBit.ConnectionSuccessful')} — Web API ${data.version}` })
      } else {
        setTestResult({ success: false, msg: data.error })
      }
    } catch (e) {
      setTestResult({ success: false, msg: e.message })
    }
    setTesting(false)
  }

  const handleCreateCategories = async () => {
    setCreatingCategories(true)
    setTestResult(null)
    try {
      const { data } = await axios.post(qbitCategoriesHost())
      if (data.success) {
        setTestResult({ success: true, msg: t('QBit.CategoriesCreated') })
      } else {
        setTestResult({ success: false, msg: data.error })
      }
    } catch (e) {
      setTestResult({ success: false, msg: e.message })
    }
    setCreatingCategories(false)
  }

  return (
    <SecondarySettingsContent>
      <SettingSectionLabel>{t('QBit.Settings')}</SettingSectionLabel>
      <FormGroup>
        <FormControlLabel
          control={
            <Switch checked={Enabled} onChange={e => handleChange('Enabled', e.target.checked)} color='secondary' />
          }
          label={t('QBit.Enable')}
          labelPlacement='start'
        />
        <FormHelperText margin='none'>{t('QBit.EnableHint')}</FormHelperText>
      </FormGroup>

      <div style={{ opacity: Enabled ? 1 : 0.5, pointerEvents: Enabled ? 'auto' : 'none' }}>
        <TextField
          label={t('QBit.URL')}
          value={URL}
          onChange={e => handleChange('URL', e.target.value)}
          placeholder='http://localhost:8080'
          variant='outlined'
          size='small'
          fullWidth
          margin='normal'
        />
        <TextField
          label={t('QBit.Username')}
          value={Username}
          onChange={e => handleChange('Username', e.target.value)}
          variant='outlined'
          size='small'
          fullWidth
          margin='normal'
        />
        <TextField
          label={t('QBit.Password')}
          value={Password}
          onChange={e => handleChange('Password', e.target.value)}
          type='password'
          variant='outlined'
          size='small'
          fullWidth
          margin='normal'
        />

        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10, marginTop: 10, marginBottom: 10 }}>
          <Button
            variant='outlined'
            color='secondary'
            onClick={handleTest}
            disabled={!URL || testing}
            style={{ flex: '1 1 auto', minWidth: 100 }}
          >
            {testing ? <CircularProgress size={24} color='inherit' /> : t('QBit.Test')}
          </Button>
          <Button
            variant='outlined'
            color='secondary'
            onClick={handleCreateCategories}
            disabled={!URL || creatingCategories}
            style={{ flex: '1 1 auto', minWidth: 100 }}
          >
            {creatingCategories ? <CircularProgress size={24} color='inherit' /> : t('QBit.CreateCategories')}
          </Button>
        </div>
        <FormHelperText margin='none' style={{ marginBottom: '10px' }}>
          {t('QBit.TestHint')}
        </FormHelperText>
        {testResult && (
          <Typography variant='caption' style={{ color: testResult.success ? 'green' : 'red' }}>
            {testResult.msg}
          </Typography>
        )}

        <TextField
          label={t('QBit.Tags')}
          value={Tags}
          onChange={e => handleChange('Tags', e.target.value)}
          variant='outlined'
          size='small'
          fullWidth
          margin='normal'
        />
        <TextField
          label={t('QBit.SavePath')}
          value={SavePath}
          onChange={e => handleChange('SavePath', e.target.value)}
          helperText={t('QBit.SavePathHint')}
          variant='outlined'
          size='small'
          fullWidth
          margin='normal'
        />

        <FormGroup>
          <FormControlLabel
            control={
              <Switch
                checked={SequentialDownload}
                onChange={e => handleChange('SequentialDownload', e.target.checked)}
                color='secondary'
              />
            }
            label={t('QBit.SequentialDownload')}
            labelPlacement='start'
          />
          <FormHelperText margin='none'>{t('QBit.SequentialDownloadHint')}</FormHelperText>
        </FormGroup>

        <FormGroup>
          <FormControlLabel
            control={
              <Switch
                checked={FirstLastPiecePrio}
                onChange={e => handleChange('FirstLastPiecePrio', e.target.checked)}
                color='secondary'
              />
            }
            label={t('QBit.FirstLastPiecePrio')}
            labelPlacement='start'
          />
          <FormHelperText margin='none'>{t('QBit.FirstLastPiecePrioHint')}</FormHelperText>
        </FormGroup>

        <FormGroup>
          <FormControlLabel
            control={
              <Switch checked={AutoLocal} onChange={e => handleChange('AutoLocal', e.target.checked)} color='secondary' />
            }
            label={t('QBit.AutoLocal')}
            labelPlacement='start'
          />
          <FormHelperText margin='none'>{t('QBit.AutoLocalHint')}</FormHelperText>
        </FormGroup>

        <FormGroup>
          <FormControlLabel
            control={
              <Switch
                checked={AutoImport}
                onChange={e => handleChange('AutoImport', e.target.checked)}
                color='secondary'
              />
            }
            label={t('QBit.AutoImport')}
            labelPlacement='start'
          />
          <FormHelperText margin='none'>{t('QBit.AutoImportHint')}</FormHelperText>
        </FormGroup>

        <SettingSectionLabel style={{ marginTop: '20px' }}>{t('QBit.PathMaps')}</SettingSectionLabel>
        <FormHelperText margin='none' style={{ marginBottom: '10px' }}>
          {t('QBit.PathMapsHint')}
        </FormHelperText>

        <List dense>
          {(PathMaps || []).map((map, index) => (
            <ListItem key={`${map.From}-${map.To}`} style={{ paddingLeft: 0, paddingRight: 48 }}>
              <ListItemText primary={`${map.From} → ${map.To}`} />
              <ListItemSecondaryAction>
                <IconButton edge='end' aria-label='delete' onClick={() => handleDeletePathMap(index)}>
                  <DeleteIcon />
                </IconButton>
              </ListItemSecondaryAction>
            </ListItem>
          ))}
        </List>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 10, marginTop: 10 }}>
          <TextField
            label={t('QBit.From')}
            value={newFrom}
            onChange={e => setNewFrom(e.target.value)}
            placeholder='/downloads'
            variant='outlined'
            size='small'
            fullWidth
          />
          <TextField
            label={t('QBit.To')}
            value={newTo}
            onChange={e => setNewTo(e.target.value)}
            placeholder='/mnt/downloads'
            variant='outlined'
            size='small'
            fullWidth
          />
          <Button
            variant='contained'
            color='secondary'
            onClick={handleAddPathMap}
            disabled={!newFrom || !newTo}
            style={{ alignSelf: 'flex-start' }}
          >
            {t('QBit.AddMapping')}
          </Button>
        </div>
      </div>
      <br />
    </SecondarySettingsContent>
  )
}
